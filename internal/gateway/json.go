package gateway

import (
	"encoding/json"
	"io"
)

func jsonNewEncoder(w io.Writer) *json.Encoder {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder
}
