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
 * autofill id, hints, input type, resource id, hint text and HTML attributes, in traversal order;
 * plus the app package and (from browsers only, see {@link AutofillTarget}) the web domain.
 */
final class AutofillParser {
    final List<FormFields.Field> fields = new ArrayList<>();
    final FormFields.Form form;
    final String pkg;
    final String url;
    private String webDomain, webScheme;

    AutofillParser(AssistStructure s) {
        pkg = s.getActivityComponent() == null ? null : s.getActivityComponent().getPackageName();
        for (int i = 0; i < s.getWindowNodeCount(); i++) walk(s.getWindowNodeAt(i).getRootViewNode());
        form = FormFields.parse(fields);
        url = AutofillTarget.url(pkg, webDomain, webScheme);
    }

    private void walk(AssistStructure.ViewNode n) {
        if (n == null) return;
        if (webDomain == null && n.getWebDomain() != null && !n.getWebDomain().isEmpty()) {
            webDomain = n.getWebDomain();
            if (android.os.Build.VERSION.SDK_INT >= 28) webScheme = n.getWebScheme();
        }
        AutofillId id = n.getAutofillId();
        if (id != null && n.getAutofillType() == View.AUTOFILL_TYPE_TEXT && n.getVisibility() == View.VISIBLE) {
            FormFields.Field f = new FormFields.Field(id);
            f.hints = n.getAutofillHints();
            f.inputType = n.getInputType();
            f.idEntry = n.getIdEntry() == null ? "" : n.getIdEntry();
            f.hint = n.getHint() == null ? "" : n.getHint();
            f.editable = n.isEnabled();
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
            fields.add(f);
        }
        for (int i = 0; i < n.getChildCount(); i++) walk(n.getChildAt(i));
    }

    /** The ids to save or fill (username first). */
    AutofillId[] ids() {
        List<AutofillId> out = new ArrayList<>();
        for (FormFields.Field f : form.all()) out.add((AutofillId) f.id);
        return out.toArray(new AutofillId[0]);
    }
}
