// Package pdffill provides AI-powered PDF form filling for AgentScript.
//
// Flow:
//  1. Extract fillable fields from a PDF template (pypdf)
//  2. Send field names + user data to Gemini for intelligent mapping
//  3. Fill the PDF with mapped values (pypdf)
//
// Commands:
//
//	pdf_fields "template.pdf"           — list all form fields
//	pdf_fill "template.pdf" "data.txt"  — AI-powered fill
//
// Pipeline examples:
//
//	read "data.txt" >=> pdf_fill "form.pdf"
//	pdf_fields "form.pdf" >=> ask "what data does this form need?"
//	( read "data.txt" <*> pdf_fields "form.pdf" ) >=> merge >=> pdf_fill "form.pdf"
package pdffill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PDFFormField represents a single form field extracted from a PDF.
type PDFFormField struct {
	FieldID        string         `json:"field_id"`
	Page           int            `json:"page"`
	Type           string         `json:"type"`
	Rect           []float64      `json:"rect,omitempty"`
	CheckedValue   string         `json:"checked_value,omitempty"`
	UncheckedValue string         `json:"unchecked_value,omitempty"`
	RadioOptions   []RadioOption  `json:"radio_options,omitempty"`
	ChoiceOptions  []ChoiceOption `json:"choice_options,omitempty"`
}

// RadioOption represents a single radio button option.
type RadioOption struct {
	Value string    `json:"value"`
	Rect  []float64 `json:"rect,omitempty"`
}

// ChoiceOption represents a single dropdown/choice option.
type ChoiceOption struct {
	Value string `json:"value"`
	Text  string `json:"text"`
}

// FieldValue represents a field with its assigned value for filling.
type FieldValue struct {
	FieldID     string `json:"field_id"`
	Description string `json:"description,omitempty"`
	Page        int    `json:"page"`
	Value       string `json:"value"`
}

// Reasoner is the functional seam for AI calls.
// Matches Gemini's GenerateContent signature.
type Reasoner func(ctx context.Context, prompt string) (string, error)

// --- Embedded Python scripts ---
// Using strings.Join to avoid backtick issues in git patches.

var extractFieldsPy = strings.Join([]string{
	"import json, sys",
	"from pypdf import PdfReader",
	"",
	"def get_full_id(ann):",
	"    c = []",
	"    while ann:",
	"        n = ann.get('/T')",
	"        if n: c.append(n)",
	"        ann = ann.get('/Parent')",
	"    return '.'.join(reversed(c)) if c else None",
	"",
	"def make_field(field, fid):",
	"    d = {'field_id': fid}",
	"    ft = field.get('/FT')",
	"    if ft == '/Tx':",
	"        d['type'] = 'text'",
	"    elif ft == '/Btn':",
	"        d['type'] = 'checkbox'",
	"        st = field.get('/_States_', [])",
	"        if len(st) == 2:",
	"            if '/Off' in st:",
	"                d['checked_value'] = st[0] if st[0] != '/Off' else st[1]",
	"                d['unchecked_value'] = '/Off'",
	"            else:",
	"                d['checked_value'] = st[0]",
	"                d['unchecked_value'] = st[1]",
	"    elif ft == '/Ch':",
	"        d['type'] = 'choice'",
	"        st = field.get('/_States_', [])",
	"        d['choice_options'] = [{'value': s[0], 'text': s[1]} for s in st]",
	"    else:",
	"        d['type'] = 'unknown'",
	"    return d",
	"",
	"def extract(pdf_path):",
	"    reader = PdfReader(pdf_path)",
	"    fields = reader.get_fields()",
	"    if not fields: return []",
	"    by_id = {}",
	"    radios = set()",
	"    for fid, f in fields.items():",
	"        if f.get('/Kids'):",
	"            if f.get('/FT') == '/Btn': radios.add(fid)",
	"            continue",
	"        by_id[fid] = make_field(f, fid)",
	"    radio_by_id = {}",
	"    for pi, page in enumerate(reader.pages):",
	"        for ann in page.get('/Annots', []):",
	"            fid = get_full_id(ann)",
	"            if fid in by_id:",
	"                by_id[fid]['page'] = pi + 1",
	"                r = ann.get('/Rect')",
	"                if r: by_id[fid]['rect'] = [float(x) for x in r]",
	"            elif fid in radios:",
	"                try: on = [v for v in ann['/AP']['/N'] if v != '/Off']",
	"                except: continue",
	"                if len(on) == 1:",
	"                    r = ann.get('/Rect')",
	"                    if fid not in radio_by_id:",
	"                        radio_by_id[fid] = {'field_id': fid, 'type': 'radio_group', 'page': pi+1, 'radio_options': []}",
	"                    radio_by_id[fid]['radio_options'].append({'value': on[0], 'rect': [float(x) for x in r] if r else None})",
	"    res = [f for f in by_id.values() if 'page' in f]",
	"    res.extend(radio_by_id.values())",
	"    res.sort(key=lambda f: (f.get('page',0), f.get('rect',[0,0,0,0])[1] if f.get('rect') else 0))",
	"    return res",
	"",
	"if __name__ == '__main__':",
	"    fields = extract(sys.argv[1])",
	"    with open(sys.argv[2], 'w') as f: json.dump(fields, f, indent=2)",
	"    print(json.dumps({'count': len(fields), 'fields': fields}))",
}, "\n")

var fillFieldsPy = strings.Join([]string{
	"import json, sys",
	"from pypdf import PdfReader, PdfWriter",
	"from pypdf.generic import DictionaryObject",
	"from pypdf.constants import FieldDictionaryAttributes",
	"",
	"def monkeypatch():",
	"    orig = DictionaryObject.get_inherited",
	"    def patched(self, key, default=None):",
	"        r = orig(self, key, default)",
	"        if key == FieldDictionaryAttributes.Opt:",
	"            if isinstance(r, list) and all(isinstance(v, list) and len(v)==2 for v in r):",
	"                r = [x[0] for x in r]",
	"        return r",
	"    DictionaryObject.get_inherited = patched",
	"",
	"def fill(inp, fvj, out):",
	"    monkeypatch()",
	"    with open(fvj) as f: fields = json.load(f)",
	"    by_page = {}",
	"    for fld in fields:",
	"        if 'value' in fld and fld['value']:",
	"            p = fld['page']",
	"            if p not in by_page: by_page[p] = {}",
	"            by_page[p][fld['field_id']] = fld['value']",
	"    reader = PdfReader(inp)",
	"    writer = PdfWriter(clone_from=reader)",
	"    for p, fv in by_page.items():",
	"        writer.update_page_form_field_values(writer.pages[p-1], fv, auto_regenerate=False)",
	"    writer.set_need_appearances_writer(True)",
	"    with open(out, 'wb') as f: writer.write(f)",
	"    print(json.dumps({'status':'ok','output':out,'filled':sum(len(v) for v in by_page.values())}))",
	"",
	"if __name__ == '__main__': fill(sys.argv[1], sys.argv[2], sys.argv[3])",
}, "\n")

// ensurePyDeps installs pypdf if not already available.
func ensurePyDeps() error {
	cmd := exec.Command("python3", "-c", "import pypdf")
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "[pdffill] Installing pypdf...")
		install := exec.Command("pip3", "install", "--break-system-packages", "-q", "pypdf")
		install.Stdout = os.Stderr
		install.Stderr = os.Stderr
		return install.Run()
	}
	return nil
}

// writeTempScript writes a Python script to a temp file and returns the path.
func writeTempScript(name, content string) (string, error) {
	path := filepath.Join(os.TempDir(), name)
	return path, os.WriteFile(path, []byte(content), 0755)
}

// ExtractFields extracts all form fields from a PDF.
func ExtractFields(ctx context.Context, pdfPath string, verbose bool) (string, error) {
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		return "", fmt.Errorf("PDF not found: %s", pdfPath)
	}
	if err := ensurePyDeps(); err != nil {
		return "", fmt.Errorf("failed to install pypdf: %w", err)
	}
	sp, err := writeTempScript("as_extract_fields.py", extractFieldsPy)
	if err != nil {
		return "", err
	}
	oj := filepath.Join(os.TempDir(), "as_field_info.json")
	cmd := exec.CommandContext(ctx, "python3", sp, pdfPath, oj)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("field extraction failed: %s\n%w", string(out), err)
	}
	data, err := os.ReadFile(oj)
	if err != nil {
		return "", err
	}
	var fields []PDFFormField
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d form fields in %s\n\n", len(fields), filepath.Base(pdfPath)))
	for i, f := range fields {
		sb.WriteString(fmt.Sprintf("  %d. [page %d] %-30s type: %s", i+1, f.Page, f.FieldID, f.Type))
		if f.Type == "checkbox" {
			sb.WriteString(fmt.Sprintf("  (check=%s)", f.CheckedValue))
		}
		if f.Type == "radio_group" && len(f.RadioOptions) > 0 {
			opts := make([]string, len(f.RadioOptions))
			for j, o := range f.RadioOptions {
				opts[j] = o.Value
			}
			sb.WriteString(fmt.Sprintf("  options=%v", opts))
		}
		if f.Type == "choice" && len(f.ChoiceOptions) > 0 {
			opts := make([]string, len(f.ChoiceOptions))
			for j, o := range f.ChoiceOptions {
				opts[j] = o.Text
			}
			sb.WriteString(fmt.Sprintf("  options=%v", opts))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n---\n")
	sb.WriteString(string(data))
	return sb.String(), nil
}

// FillForm reads data, extracts PDF fields, uses AI to map data to fields,
// then fills and saves the PDF.
func FillForm(ctx context.Context, reasoner Reasoner, pdfPath, dataPath, pipeInput string, verbose bool) (string, error) {
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		return "", fmt.Errorf("PDF not found: %s", pdfPath)
	}
	if err := ensurePyDeps(); err != nil {
		return "", fmt.Errorf("failed to install pypdf: %w", err)
	}

	// Step 1: Read the data
	var userData string
	if dataPath != "" {
		d, err := os.ReadFile(dataPath)
		if err != nil {
			return "", fmt.Errorf("failed to read data file %s: %w", dataPath, err)
		}
		userData = string(d)
	}
	if pipeInput != "" {
		if userData != "" {
			userData += "\n\n--- Additional Context ---\n" + pipeInput
		} else {
			userData = pipeInput
		}
	}
	if userData == "" {
		return "", fmt.Errorf("no data provided: pass a data file path or pipe data via >=>")
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[pdffill] Data loaded: %d bytes\n", len(userData))
	}

	// Step 2: Extract PDF form fields
	sp, err := writeTempScript("as_extract_fields.py", extractFieldsPy)
	if err != nil {
		return "", err
	}
	fij := filepath.Join(os.TempDir(), "as_field_info.json")
	cmd := exec.CommandContext(ctx, "python3", sp, pdfPath, fij)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("field extraction failed: %s\n%w", string(out), err)
	}
	fd, err := os.ReadFile(fij)
	if err != nil {
		return "", err
	}
	var fields []PDFFormField
	if err := json.Unmarshal(fd, &fields); err != nil {
		return "", err
	}
	if len(fields) == 0 {
		return "", fmt.Errorf("no fillable form fields found in %s", pdfPath)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[pdffill] Found %d form fields\n", len(fields))
	}

	// Step 3: Use AI to map data to fields
	if reasoner == nil {
		return "", fmt.Errorf("no AI backend available (need GEMINI_API_KEY or CLAUDE_API_KEY)")
	}
	fdesc := buildFieldDescriptions(fields)
	prompt := buildMappingPrompt(fdesc, userData)
	if verbose {
		fmt.Fprintf(os.Stderr, "[pdffill] Sending %d fields + data to AI for mapping...\n", len(fields))
	}
	aiResp, err := reasoner(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("AI mapping failed: %w", err)
	}
	mappingJSON := cleanJSONResponse(aiResp)
	var fieldValues []FieldValue
	if err := json.Unmarshal([]byte(mappingJSON), &fieldValues); err != nil {
		return "", fmt.Errorf("failed to parse AI response: %w\nRaw: %s", err, mappingJSON)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[pdffill] AI mapped %d values\n", len(fieldValues))
	}

	// Step 4: Fill the PDF
	fvp := filepath.Join(os.TempDir(), "as_field_values.json")
	fvd, _ := json.MarshalIndent(fieldValues, "", "  ")
	if err := os.WriteFile(fvp, fvd, 0644); err != nil {
		return "", err
	}
	fs, err := writeTempScript("as_fill_fields.py", fillFieldsPy)
	if err != nil {
		return "", err
	}
	ext := filepath.Ext(pdfPath)
	base := strings.TrimSuffix(pdfPath, ext)
	outputPDF := base + "_filled" + ext
	fc := exec.CommandContext(ctx, "python3", fs, pdfPath, fvp, outputPDF)
	fo, err := fc.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("PDF fill failed: %s\n%w", string(fo), err)
	}

	// Summary
	var sb strings.Builder
	sb.WriteString("PDF form filled successfully!\n\n")
	sb.WriteString(fmt.Sprintf("  Input PDF:  %s\n", pdfPath))
	if dataPath != "" {
		sb.WriteString(fmt.Sprintf("  Data file:  %s\n", dataPath))
	}
	sb.WriteString(fmt.Sprintf("  Output PDF: %s\n", outputPDF))
	sb.WriteString(fmt.Sprintf("  Fields:     %d found, %d filled\n\n", len(fields), len(fieldValues)))
	sb.WriteString("Filled values:\n")
	for _, fv := range fieldValues {
		val := fv.Value
		if len(val) > 60 {
			val = val[:57] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %-30s = %s\n", fv.FieldID, val))
	}
	return sb.String(), nil
}

func buildFieldDescriptions(fields []PDFFormField) string {
	var sb strings.Builder
	for _, f := range fields {
		sb.WriteString(fmt.Sprintf("- field_id: %q, page: %d, type: %s", f.FieldID, f.Page, f.Type))
		if f.Type == "checkbox" {
			sb.WriteString(fmt.Sprintf(", checked_value: %q, unchecked_value: %q", f.CheckedValue, f.UncheckedValue))
		}
		if f.Type == "radio_group" {
			opts := make([]string, len(f.RadioOptions))
			for i, o := range f.RadioOptions {
				opts[i] = o.Value
			}
			sb.WriteString(fmt.Sprintf(", options: %v", opts))
		}
		if f.Type == "choice" {
			opts := make([]string, len(f.ChoiceOptions))
			for i, o := range f.ChoiceOptions {
				opts[i] = fmt.Sprintf("%s (%s)", o.Value, o.Text)
			}
			sb.WriteString(fmt.Sprintf(", options: %v", opts))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func buildMappingPrompt(fieldDescriptions, userData string) string {
	return "You are a PDF form filling assistant. Map user data to PDF form fields.\n\n" +
		"FORM FIELDS:\n" + fieldDescriptions + "\n" +
		"USER DATA:\n" + userData + "\n\n" +
		"INSTRUCTIONS:\n" +
		"1. Analyze each field_id name to determine what data it expects.\n" +
		"2. Match user data to fields. For checkboxes use exact checked_value/unchecked_value.\n" +
		"   For radio/choice use exact option values.\n" +
		"3. If a field cannot be confidently mapped, omit it.\n" +
		"4. Format dates, phones etc to match common form conventions.\n\n" +
		"Return ONLY a valid JSON array. No markdown, no explanation, no code fences.\n" +
		"Each object: {\"field_id\": \"...\", \"description\": \"...\", \"page\": N, \"value\": \"...\"}"
}

// cleanJSONResponse strips markdown code fences from AI output.
func cleanJSONResponse(text string) string {
	text = strings.TrimSpace(text)
	fence := string([]byte{96, 96, 96})
	if strings.HasPrefix(text, fence+"json") {
		text = strings.TrimPrefix(text, fence+"json")
		text = strings.TrimSuffix(text, fence)
	} else if strings.HasPrefix(text, fence) {
		text = strings.TrimPrefix(text, fence)
		text = strings.TrimSuffix(text, fence)
	}
	return strings.TrimSpace(text)
}
