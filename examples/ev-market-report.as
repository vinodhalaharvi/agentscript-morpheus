( search "Tesla EV market share 2024" >=> analyze "market position"
  <*> search "Ford EV strategy 2024" >=> analyze "market position"
  <*> search "GM electric vehicles 2024" >=> analyze "market position"
)
>=> merge
>=> ask "Write an executive summary of the EV market competition"
>=> save "ev-report.md"
>=> email "executives@company.com"
