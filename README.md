[![GitHub release](https://img.shields.io/github/release/UnitVectorY-Labs/mcp-graphql-forge.svg)](https://github.com/UnitVectorY-Labs/mcp-graphql-forge/releases/latest) [![License](https://img.shields.io/badge/license-MIT-blue)](https://opensource.org/licenses/MIT) [![Active](https://img.shields.io/badge/Status-Active-green)](https://guide.unitvectorylabs.com/bestpractices/status/#active) [![Go Report Card](https://goreportcard.com/badge/github.com/UnitVectorY-Labs/mcp-graphql-forge)](https://goreportcard.com/report/github.com/UnitVectorY-Labs/mcp-graphql-forge)

# mcp-graphql-forge

A lightweight, configuration-driven MCP server that exposes curated GraphQL queries as modular tools, enabling intentional API interactions from your agents.

## Purpose

`mcp-graphql-forge` lets you turn any GraphQL endpoint into an MCP server whose tools are defined in YAML files that specify the GraphQL queries and their parameters. This allows you to create a modular, secure, and minimal server that can be easily extended without modifying the application code.

## Configuration

The server is configured using environment variables and YAML files.

### Environment Variables

- `FORGE_CONFIG`: Specifies the path to the folder containing the YAML configuration files (`forge.yaml` and tool definitions). Defaults to the current directory (`.`) if not set.
- `FORGE_DEBUG`: If set to `true` (case-insensitive), enables detailed debug logging to `stderr`, including the obtained token and the full HTTP request/response for GraphQL calls. Defaults to `false`.

### forge.yaml

The configuration folder uses a special configuration file `forge.yaml` that specifies the common configuration attributes.

The following attributes can be specified in the file:

- `name`: The name of the MCP server
- `version`: The version of the MCP server
- `url`: The URL of the GraphQL endpoint
- `token_command`: The command to use to request the Bearer token for the `Authorization` header (optional)

An example configuration would look like:

```yaml
name: "ExampleServer"
version: "0.1.0"
url: "https://api.github.com/graphql"
token_command: "gh auth token"
```

### Tool Configuration

All other YAML files located in the folder are treated as configuration files. Each YAML file defines a tool for the MCP server.


The following attributes can be specified in the file:

- `name`: The name of the MCP tool
- `description`: The description of the MCP tool
- `query`: The GraphQL query to execute
- `inputs`: The list of inputs defined by the MCP tool and passed into the GraphQL query as variables
  - `name`: The name of the input
  - `type`: The parameter type; can be 'string' or 'number'
  - `description`: The description of the parameter for the MCP tool to use
  - `required`: Boolean value specifying if the attribute is required

An example configuration would look like:

```yaml
name: "getUser"
description: "Fetch basic information about a user by `login`, including their name, URL, and location."
query: |
  query ($login: String!) {
    user(login: $login) {
      id
      name
      url
      location
    }
  }
inputs:
  - name: "login"
    type: "string"
    description: "The user `login` that uniquely identifies their account."
    required: true
```

### Run in SSE Mode

By default the server runs in stdio mode, but if you want to run in SSE mode, you can specify the `--sse` command line flag specifying the server name and port (ex: localhost:8080).  This will run with the following endpoints that your MCP client can connect to:

- SSE Endpoint: /mcp/sse
- Message Endpoint: /mcp/message

## Limitations

- Each instance of `mcp-graphql-forge` can only be used with a single GraphQL server at a single URL.
- All requests use the same Authorization header in the form of a Bearer token.
- The GraphQL queries are all exposed as Tools and not as Resources, even if they are not mutations. This is because not all MCP clients currently support Resources.
