# pdf-fill-advanced.as — Inspect fields + gather data, then fill
# Usage: make run-file FILE=examples/pdf-fill-advanced.as
( read "applicant-data.txt"
  <*> pdf_fields "application-form.pdf"
)
>=> merge
>=> pdf_fill "application-form.pdf"
