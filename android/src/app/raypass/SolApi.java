package app.raypass;

import app.raypass.core.Json;

import java.io.BufferedReader;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * Sol's app API (design/API.md in Sol) over HttpURLConnection on loopback, with the token from
 * the CONNECT handshake. Only what Raypass needs: whoami, feed read and append, and the events
 * stream. Request and response bodies are never logged (vault bodies are ciphertext, but the
 * rule is simpler without exceptions). Errors are {@link SolException} with Sol's code.
 */
final class SolApi {
    /** An error answer from Sol, or a transport failure (code "unavailable", status 0). */
    static final class SolException extends IOException {
        final String code;
        final int status;

        SolException(int status, String code, String message) {
            super(code + (message == null || message.isEmpty() ? "" : ": " + message));
            this.status = status;
            this.code = code;
        }

        boolean unauthorized() { return status == 401 || "unauthorized".equals(code); }
    }

    /** One event of the stream. */
    static final class Event {
        String id = "", type = "", data = "";
    }

    /** Receives stream events; return false to stop. */
    interface EventSink {
        /** Called once the stream is open (headers received), before any event. */
        void opened() throws IOException;

        boolean event(Event e) throws IOException;
    }

    private final String api;
    private final String token;
    private volatile HttpURLConnection stream;

    SolApi(String api, String token) {
        this.api = api.endsWith("/") ? api.substring(0, api.length() - 1) : api;
        this.token = token;
    }

    private HttpURLConnection open(String method, String path) throws IOException {
        URL u = new URL(api + path);
        String host = u.getHost();
        // The token goes only to loopback, whatever a corrupted pref says.
        if (!host.equals("127.0.0.1") && !host.equals("localhost") && !host.equals("::1") && !host.equals("[::1]")) {
            throw new SolException(0, "bad_request", "Sol's API must be on 127.0.0.1");
        }
        HttpURLConnection c = (HttpURLConnection) u.openConnection();
        c.setRequestMethod(method);
        c.setUseCaches(false);
        c.setConnectTimeout(5000);
        c.setReadTimeout(30000);
        c.setRequestProperty("Authorization", "Bearer " + token);
        return c;
    }

    private static String readAll(InputStream in) throws IOException {
        if (in == null) return "";
        ByteArrayOutputStream b = new ByteArrayOutputStream();
        byte[] buf = new byte[8192];
        try {
            for (int n; (n = in.read(buf)) > 0; ) b.write(buf, 0, n);
        } finally {
            in.close();
        }
        return new String(b.toByteArray(), StandardCharsets.UTF_8);
    }

    private static SolException error(HttpURLConnection c, int status) {
        String code = status == 401 ? "unauthorized" : status == 403 ? "forbidden" : status == 404 ? "not_found" : "unavailable";
        String msg = "";
        try {
            Map<String, Object> e = Json.obj(Json.obj(Json.parse(readAll(c.getErrorStream()))).get("error"));
            code = Json.str(e, "code", code);
            msg = Json.str(e, "message", "");
        } catch (Exception ignored) {
            // Not a JSON error body: keep the status-derived code.
        }
        return new SolException(status, code, msg);
    }

    private Object call(String method, String path, Object body) throws IOException {
        HttpURLConnection c = open(method, path);
        try {
            if (body != null) {
                byte[] b = Json.write(body).getBytes(StandardCharsets.UTF_8);
                c.setDoOutput(true);
                c.setRequestProperty("Content-Type", "application/json");
                c.setFixedLengthStreamingMode(b.length);
                OutputStream o = c.getOutputStream();
                o.write(b);
                o.close();
            }
            int status;
            try {
                status = c.getResponseCode();
            } catch (IOException e) {
                throw new SolException(0, "unavailable", "Sol is not reachable");
            }
            if (status / 100 != 2) throw error(c, status);
            try {
                return Json.parse(readAll(c.getInputStream()));
            } catch (IllegalArgumentException e) {
                throw new SolException(status, "bad_response", "Sol answered something that is not JSON");
            }
        } catch (SolException e) {
            throw e;
        } catch (IOException e) {
            throw new SolException(0, "unavailable", "Sol is not reachable");
        } finally {
            c.disconnect();
        }
    }

    /** GET /v1/whoami. */
    Map<String, Object> whoami() throws IOException { return Json.obj(call("GET", "/v1/whoami", null)); }

    /** GET /v1/feeds/{address}?from=&limit= : entries with pos >= from. */
    List<Map<String, Object>> read(String address, long from, int limit) throws IOException {
        List<Map<String, Object>> out = new ArrayList<>();
        Object v = call("GET", "/v1/feeds/" + address + "?from=" + from + "&limit=" + limit, null);
        for (Map<String, Object> e : Json.objects(v)) out.add(e);
        return out;
    }

    /** POST /v1/feeds/{feed}/append : the stored entry (with pos). */
    Map<String, Object> append(String feed, Map<String, Object> body) throws IOException {
        return Json.obj(call("POST", "/v1/feeds/" + feed + "/append", Json.map("body", body)));
    }

    /**
     * GET /v1/events?feeds=... as server-sent events until the sink returns false, the stream
     * ends or {@link #closeStream} is called. Resumes after lastEventId when given.
     */
    void events(List<String> feeds, String lastEventId, EventSink sink) throws IOException {
        StringBuilder q = new StringBuilder();
        for (String f : feeds) q.append(q.length() == 0 ? "" : ",").append(f);
        HttpURLConnection c = open("GET", "/v1/events?feeds=" + URLEncoder.encode(q.toString(), "UTF-8"));
        c.setReadTimeout(0); // a quiet stream is normal; Sol closes it if it goes away
        c.setRequestProperty("Accept", "text/event-stream");
        if (lastEventId != null && !lastEventId.isEmpty()) c.setRequestProperty("Last-Event-ID", lastEventId);
        stream = c;
        try {
            int status;
            try {
                status = c.getResponseCode();
            } catch (IOException e) {
                throw new SolException(0, "unavailable", "Sol is not reachable");
            }
            if (status / 100 != 2) throw error(c, status);
            sink.opened();
            BufferedReader r = new BufferedReader(new InputStreamReader(c.getInputStream(), StandardCharsets.UTF_8));
            Event ev = new Event();
            StringBuilder data = new StringBuilder();
            boolean hasData = false;
            for (String line; (line = r.readLine()) != null; ) {
                if (line.isEmpty()) {
                    if (!ev.type.isEmpty()) {
                        ev.data = data.toString();
                        if (!sink.event(ev)) return;
                    }
                    ev = new Event();
                    data.setLength(0);
                    hasData = false;
                } else if (line.startsWith(":")) {
                    // comment / keep-alive
                } else if (line.startsWith("id:")) {
                    ev.id = line.substring(3).trim();
                } else if (line.startsWith("event:")) {
                    ev.type = line.substring(6).trim();
                } else if (line.startsWith("data:")) {
                    String d = line.substring(5);
                    if (d.startsWith(" ")) d = d.substring(1);
                    if (hasData) data.append('\n');
                    data.append(d);
                    hasData = true;
                }
            }
        } catch (SolException e) {
            throw e;
        } catch (IOException e) {
            throw new SolException(0, "unavailable", "the events stream ended");
        } finally {
            stream = null;
            c.disconnect();
        }
    }

    /** Ends a running events stream (from another thread). */
    void closeStream() {
        HttpURLConnection c = stream;
        if (c != null) c.disconnect();
    }
}
