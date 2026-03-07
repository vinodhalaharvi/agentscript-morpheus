( ( search "React framework pros cons" >=> analyze
    <*> search "Vue framework pros cons" >=> analyze
    <*> search "Angular framework pros cons" >=> analyze
  ) >=> merge >=> ask "summarize frontend frameworks"
  <*> ( search "Node.js backend" >=> analyze
        <*> search "Go backend" >=> analyze
        <*> search "Rust backend" >=> analyze
      ) >=> merge >=> ask "summarize backend options"
) >=> merge >=> ask "Create a full-stack technology recommendation"
