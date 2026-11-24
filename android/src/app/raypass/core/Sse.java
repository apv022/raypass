package app.raypass.core;

/**
 * A server-sent events parser (the WHATWG "event stream interpretation"), fed one line at a time
 * with the line terminator removed. Pure Java so the JDK tests cover it. What matters for Sol's
 * events stream: several {@code data:} lines join with "\n", one leading space after the colon is
 * dropped, comment lines ({@code : beat}) are ignored, an event with no {@code event:} field is a
 * "message", and the last event id carries over to later events until a new {@code id:} line,
 * which is what a reconnect sends back as {@code Last-Event-ID}.
 */
public final class Sse {
    /** One dispatched event. */
    public static final class Event {
        /** The last event id seen on the stream ("" if none yet). */
        public final String id;
        /** The event type ("message" when the stream named none). */
        public final String type;
        /** The data lines joined with "\n". */
        public final String data;

        Event(String id, String type, String data) {
            this.id = id;
            this.type = type;
            this.data = data;
        }
    }

    private String lastId = "";
    private String type = "";
    private final StringBuilder data = new StringBuilder();
    private boolean hasData;

    /** The last event id seen so far ("" if none). */
    public String lastId() { return lastId; }

    /**
     * Feeds one line (without its line terminator); returns the event an empty line dispatches,
     * or null.
     */
    public Event line(String line) {
        if (line.isEmpty()) {
            Event e = null;
            if (hasData) e = new Event(lastId, type.isEmpty() ? "message" : type, data.toString());
            type = "";
            data.setLength(0);
            hasData = false;
            return e;
        }
        if (line.charAt(0) == ':') return null; // comment / heartbeat
        int c = line.indexOf(':');
        String field = c < 0 ? line : line.substring(0, c);
        String value = c < 0 ? "" : line.substring(c + 1);
        if (value.startsWith(" ")) value = value.substring(1);
        switch (field) {
            case "event": type = value; break;
            case "data":
                if (hasData) data.append('\n');
                data.append(value);
                hasData = true;
                break;
            case "id":
                if (value.indexOf('\0') < 0) lastId = value;
                break;
            default: break; // "retry" and unknown fields: ignored
        }
        return null;
    }
}
