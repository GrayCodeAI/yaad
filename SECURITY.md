# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Yaad, please report it responsibly:

**Email**: security@graycode.ai  
**Response time**: We aim to respond within 48 hours.

Please do **not** open a public GitHub issue for security vulnerabilities.

## Security Practices

### Data Privacy
- **Local-first**: All data stays on your machine. Yaad never sends data to external servers.
- **No LLM calls**: Yaad is a memory layer — it does not call any LLM APIs. Your code never leaves your machine through Yaad.
- **Privacy filtering**: API keys, tokens, secrets, and private keys are automatically stripped on ingest before storage.

### Encryption
- **At rest**: Optional AES-256-GCM encryption for the SQLite database (`internal/encrypt/`).
- **In transit**: HTTPS/TLS support with auto-generated self-signed certificates.

### Access Control
- **Localhost only**: REST API binds to `127.0.0.1` by default — not accessible from the network.
- **No authentication by default**: Yaad is a local tool. For remote/team use, enable TLS and add authentication at the reverse proxy level.

### Dependencies
- **Minimal**: Pure Go, no CGO, no C compiler required.
- **Audited**: All dependencies are well-known, actively maintained Go packages.
- **No network deps**: Core functionality requires zero network access.

## Supported Versions

| Version | Supported |
|---|---|
| 0.1.x | ✅ |

## Scope

The following are in scope for security reports:
- Data leakage (memories exposed to unauthorized parties)
- Privacy filter bypasses (secrets not stripped)
- SQL injection in SQLite queries
- Path traversal in file operations
- Denial of service via crafted input

The following are out of scope:
- Issues requiring physical access to the machine
- Issues in third-party coding agents (Hawk, Claude Code, etc.)
- Social engineering
