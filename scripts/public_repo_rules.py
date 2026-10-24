"""Known public-source token formats shared by worktree and Git-history gates."""

TOKEN_RULES = {'private-key': '-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',
 'github-token': '\\b(?:gh[pousr]_[a-zA-Z0-9_]{20,}|github_pat_[a-zA-Z0-9_]{20,})',
 'openai-token': '\\bsk-(?:proj-)?[A-Za-z0-9_-]{24,}',
 'apify-token': '\\bapify_api_[A-Za-z0-9]{20,}',
 'google-api-key': '\\bAIza[0-9A-Za-z_-]{30,}',
 'aws-key-id': '\\b(?:AKIA|ASIA)[A-Z0-9]{16}',
 'db-credentials-in-url': '(?i)\\b(?:mysql|mariadb|postgres|mongodb)(?:\\+srv)?://[^\\s]+:[^\\s]+@',
 'sites-project-identity': '\\bappgprj_[0-9a-f]{20,}'}
