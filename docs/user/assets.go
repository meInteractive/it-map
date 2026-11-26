// Package userdocs содержит материалы, которые доступны в настройках приложения.
package userdocs

import "embed"

//go:embed manifest.json USER_GUIDE.md ADMIN_GUIDE.md TRANSFER_FORMATS.md CODE_GUIDE.md API.md DATABASE.md CHANGELOG.md MAINTENANCE.md
var Files embed.FS
