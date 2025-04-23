# mcp-graphql-forge Example Configuration

This is an example configuration for the mcp-graphql-forge project. It demonstrates how to structure your configuration files to interact with a GraphQL API, specifically GitHub's GraphQL API.

This assumes you have the GitHub Command Line Interface (CLI) installed and configured with your GitHub account as it uses the `gh auth token` command to retrieve the authentication token.

## GitHub Tools

This configuration provides a `getUser` tool that retrieves a few basic attributes for the request user by calling the GraphQL API.

## Visual Studio Code Test Configuration

```json
{
  "mcp": {
    "inputs": [],
    "servers": {
      "graphql": {
        "command": "mcp-graphql-forge",
        "args": [],
        "env": {
          "FORGE_CONFIG": "mcp-graphql-forge/example"
        }
      }
    }
  }
}
```
