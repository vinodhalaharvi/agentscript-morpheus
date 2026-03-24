# pdf-fill.as — Fill a PDF form using AI
# Usage: make run-file FILE=examples/pdf-fill.as
read "applicant-data.txt" >=> pdf_fill "application-form.pdf"
