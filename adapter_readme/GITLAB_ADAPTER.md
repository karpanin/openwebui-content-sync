# GitLab Adapter

The GitLab adapter allows syncing files from GitLab repositories to OpenWebUI knowledge bases. It supports both GitLab Cloud (accessed via gitlab.com) and self-managed GitLab instances (On-Premise).

## Features

- **Project Sync**: Syncs all files from specified GitLab projects.
- **On-Premise Support**: Configurable base URL for self-managed instances.
- **Smart Filtering**: Automatically filters out binary files and syncs only text-based content.
- **Recursive Sync**: Traverses the entire directory structure of the repository.
- **Content Hashing**: Only diffs and uploads files that have changed since the last sync.

## Configuration

The adapter is configured in the `config.yaml` file under the `gitlab` section.

### Basic Configuration (GitLab Cloud)

For repositories hosted on `gitlab.com`:

```yaml
gitlab:
  enabled: true
  token: "your-gitlab-token"  # Or set via GITLAB_TOKEN env var
  mappings:
    - repository: "group/project-name"
      knowledge_id: "target-knowledge-base-id"
```

### On-Premise Configuration

For self-hosted GitLab instances, provide the `base_url`:

```yaml
gitlab:
  enabled: true
  base_url: "https://gitlab.your-company.com"
  token: "your-gitlab-token"
  mappings:
    - repository: "group/project-name"
      knowledge_id: "target-knowledge-base-id"
```

### Environment Variables

Sensitive information like tokens should ideally be set via environment variables:

| Variable | Description |
|----------|-------------|
| `GITLAB_TOKEN` | Personal Access Token with `read_api` or `read_repository` scope. |
| `GITLAB_BASE_URL` | Base URL for the GitLab instance (e.g., `https://gitlab.example.com`). Defaults to `https://gitlab.com` if empty. |

## Mappings

You can map different repositories to different knowledge bases. This is useful for organizing documentation for different projects.

```yaml
mappings:
  - repository: "engineering/backend-service"
    knowledge_id: "backend-kb"
  - repository: "engineering/frontend-app"
    knowledge_id: "frontend-kb"
```

## Authentication

You need to create a Personal Access Token (PAT) in GitLab:
1. Go to **Preferences** > **Access Tokens**.
2. Create a token with **read_api** (or at minimum `read_repository` if available/sufficient for your version).
3. Use this token in the configuration.

## How It Works

1. The adapter connects to the configured GitLab instance.
2. It iterates through the list of configured repositories.
3. For each repository, it recursively lists files.
4. It filters files based on extension (text files only) and binary check.
5. It fetches the content of matching files.
6. A SHA256 hash is calculated for the content.
7. The file is sent to the sync manager, which compares the hash with the previous sync state.
8. If changed or new, the file is uploaded to OpenWebUI and added to the specified Knowledge Base.
