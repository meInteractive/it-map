package profiles

// AccessStatus preserves legacy dispatch order after authenticating a session.
// It describes authorization and method/kind/storage guards, not provider work.
func AccessStatus(surface, kind, method string, signedIn, admin, storageReady bool) int {
	if !signedIn {
		return 401
	}
	if surface == "resume" {
		if method != "GET" && method != "PUT" {
			return 405
		}
		if !storageReady {
			return 503
		}
		return 200
	}
	if surface != "prompts" && surface != "models" {
		return 404
	}
	if !admin {
		return 403
	}
	if !storageReady {
		return 503
	}
	if kind == "" && method == "GET" {
		return 200
	}
	if !KnownKind(kind) {
		return 404
	}
	if method != "PUT" {
		return 405
	}
	return 200
}
