package backup

import "encoding/json"

// StringifyParsedJSON shares the accepted source ECMAScript codec with callers
// that persist JSON text outside backup: last duplicate wins, numeric property
// order, IEEE numbers and lone UTF16. It performs no catalog transform or SQL.
func StringifyParsedJSON(data []byte) (json.RawMessage, error) {
	value, e := orderedJSON(data)
	if e != nil {
		return nil, e
	}
	return rawValue(value), nil
}
