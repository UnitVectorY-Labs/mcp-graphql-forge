# mcp-graphql-forge

A lightweight, configuration-driven MCP server that exposes curated GraphQL queries as modular tools, enabling intentional API interactions from your agents.

## Purpose

`mcp-graphql-forge` lets you turn any GraphQL endpoint into an MCP server whose tools are defined in YAML files that specify the GraphQL queries and their parameters. This allows you to create a modular, secure, and minimal server that can be easily extended without modifying the application code.

## Configuration

To configure the MCP server you must specify an environment variable `FORGE_CONFIG` that points to the folder that contains the YAML configuration files for configuring the server.

### forge.yaml

The configuration folder uses a special configuration file `forge.yaml` that specifies the common configuration attributes.

```yaml
mcp_server:
  name: "ExampleServer"
  version: "0.1.0"
graphql:
  url: "https://api.github.com/graphql"
auth:
  token_script: "gh auth token"
```

### Tool Configuration

All other YAML files located in the folder are treated as configuration files.

```yaml
tool:
  name: "getUser"
  description: "Fetch basic information about user by 'login' including their name, url, and location."
graphql:
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
    description: "The user 'login' that uniquely identifies their account."
    required: true
```
