module github.com/GoCodeAlone/workflow/mcp_server

go 1.24.1

require (
	github.com/GoCodeAlone/modular v1.2.2 // Adjust version as needed
	github.com/GoCodeAlone/workflow v0.0.0-00010101000000-000000000000
)

require (
	github.com/BurntSushi/toml v1.5.0 // indirect
	github.com/golobby/cast v1.3.3 // indirect
	github.com/golobby/config/v3 v3.4.2 // indirect
	github.com/golobby/dotenv v1.3.2 // indirect
	github.com/golobby/env/v2 v2.2.4 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/GoCodeAlone/workflow => ../
