// Server-only; extracted from the v218 Worker.


export const securityHeaders = {
  "cache-control": "private, no-store",
  "content-security-policy": "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://tiles.openfreemap.org; connect-src 'self' https://tiles.openfreemap.org https://fonts.openmaptiles.org; worker-src 'self' blob:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
  "referrer-policy": "no-referrer",
  "x-content-type-options": "nosniff",
  "x-frame-options": "DENY"
};

export function htmlResponse(body, status = 200, extraHeaders = {}) {
  return new Response(body, {
    status,
    headers: {
      ...securityHeaders,
      "content-type": "text/html; charset=utf-8",
      ...extraHeaders
    }
  });
}

export function jsonResponse(value, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: {
      ...securityHeaders,
      "content-type": "application/json; charset=utf-8"
    }
  });
}

export function jsonDownloadResponse(value, filename) {
  return new Response(JSON.stringify(value, null, 2), {
    status: 200,
    headers: {
      ...securityHeaders,
      "content-type": "application/json; charset=utf-8",
      "content-disposition": `attachment; filename="${filename}"`
    }
  });
}

export function binaryDownloadResponse(value, filename) {
  return new Response(value, {
    status: 200,
    headers: {
      ...securityHeaders,
      "content-type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      "content-disposition": `attachment; filename="${filename}"`,
      "cache-control": "no-store"
    }
  });
}
