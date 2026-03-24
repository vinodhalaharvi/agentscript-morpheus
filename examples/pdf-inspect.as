# pdf-inspect.as — Extract and analyze PDF form fields
# Usage: make run-file FILE=examples/pdf-inspect.as
pdf_fields "application-form.pdf" >=> ask "summarize what data this form needs"
