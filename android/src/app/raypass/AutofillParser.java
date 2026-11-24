package app.raypass;

import android.app.assist.AssistStructure;
import android.util.Pair;
import android.view.View;
import android.view.ViewStructure;
import android.view.autofill.AutofillId;
import android.view.autofill.AutofillValue;

import app.raypass.core.AutofillTarget;
import app.raypass.core.FormFields;

import java.util.ArrayList;
import java.util.List;

/**
 * Turns an AssistStructure into what {@link FormFields} looks at: every editable text node's
 * autofill id, hints, input type, resource id, hint text, HTML attributes, focus and the web
 * domain of the page or frame it sits in (inherited from the nearest ancestor that reports one),
 * in traversal order; plus the app package. {@link AutofillTarget#resolve} then picks the URL
 * (web domains from known browsers only) and keeps only the fields of that one frame.
 */
final class AutofillParser {
    /** The fields of the chosen page or frame. */
    final List<FormFields.Field> fields;
    final FormFields.Form form;
    final String pkg;
    final String url;
    private final List<FormFields.Field> all = new ArrayList<>();

    AutofillParser(AssistStructure s) {
        pkg = s.getActivityComponent() == null ? null : s.getActivityComponent().getPackageName();
        for (int i = 0; i < s.getWindowNodeCount(); i++) walk(s.getWindowNodeAt(i).getRootViewNode(), "", "");
        AutofillTarget.Target t = AutofillTarget.resolve(pkg, all);
        url = t.url;
        fields = t.fields;
        form = FormFields.parse(fields);
    }

    private void walk(AssistStructure.ViewNode n, String domain, String scheme) {
        if (n == null) return;
        if (n.getWebDomain() != null && !n.getWebDomain().isEmpty()) {
            domain = n.getWebDomain();
            scheme = android.os.Build.VERSION.SDK_INT >= 28 && n.getWebScheme() != null ? n.getWebScheme() : "";
        }
        AutofillId id = n.getAutofillId();
        if (id != null && n.getAutofillType() == View.AUTOFILL_TYPE_TEXT && n.getVisibility() == View.VISIBLE) {
            FormFields.Field f = new FormFields.Field(id);
            f.hints = n.getAutofillHints();
            f.inputType = n.getInputType();
            f.idEntry = n.getIdEntry() == null ? "" : n.getIdEntry();
            f.hint = n.getHint() == null ? "" : n.getHint();
            f.editable = n.isEnabled();
            f.focused = n.isFocused();
            f.webDomain = domain;
            f.webScheme = scheme;
            ViewStructure.HtmlInfo h = n.getHtmlInfo();
            if (h != null) {
                if (!"input".equalsIgnoreCase(h.getTag())) f.editable = false;
                if (h.getAttributes() != null) {
                    for (Pair<String, String> a : h.getAttributes()) {
                        if (a.first == null || a.second == null) continue;
                        switch (a.first.toLowerCase(java.util.Locale.ROOT)) {
                            case "type": f.htmlType = a.second; break;
                            case "name": f.htmlName = a.second; break;
                            case "id": f.htmlId = a.second; break;
                            case "autocomplete": f.htmlAutocomplete = a.second; break;
                            default: break;
                        }
                    }
                }
            }
            AutofillValue v = n.getAutofillValue();
            if (v != null && v.isText()) f.text = v.getTextValue().toString();
            else if (n.getText() != null) f.text = n.getText().toString();
            all.add(f);
        }
        for (int i = 0; i < n.getChildCount(); i++) walk(n.getChildAt(i), domain, scheme);
    }

    /** The ids to save or fill (username first). */
    AutofillId[] ids() {
        List<AutofillId> out = new ArrayList<>();
        for (FormFields.Field f : form.all()) out.add((AutofillId) f.id);
        return out.toArray(new AutofillId[0]);
    }
}
