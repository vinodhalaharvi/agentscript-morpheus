( search "AWS vs Azure vs GCP 2024" >=> analyze "pricing"
  <*> search "cloud migration best practices" >=> analyze "strategy"
)
>=> merge
>=> ask "Create a cloud migration recommendation"
>=> doc_create "Cloud Migration Plan"
