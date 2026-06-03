package lmstudio

import (
	"encoding/json"
	"net/http"
)

// decodeJSONBody decodes the request body into v, for use by HTTP test handlers.
func decodeJSONBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
