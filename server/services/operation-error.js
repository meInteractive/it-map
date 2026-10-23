// DOMException.code is a legacy NUMBER (TimeoutError=23, AbortError=20),
// not an application/provider code. Check the exception name first.
export function operationErrorCode(error, fallback = 'storage') {
 if (['AbortError', 'TimeoutError'].includes(error?.name)) return 'timeout';
 return typeof error?.code === 'string' && error.code ? error.code : fallback;
}
