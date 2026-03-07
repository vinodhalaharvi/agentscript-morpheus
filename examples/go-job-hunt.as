// Go Contract Job Hunt
( job_search "golang contract developer" "remote"
  <*> job_search "go software engineer contract" "remote"
)
>=> merge
>=> ask "Deduplicate, format as table sorted by salary"
>=> save "go-jobs.md"
