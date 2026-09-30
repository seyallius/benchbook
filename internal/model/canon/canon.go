// Package canon. canon - JSON normalisation that sorts every object key
// lexicographically. Go already emits struct fields in declaration order, but maps
// (notably sample `extra` metrics) are emitted in random order, which would make
// output non-reproducible. Sorting here is the cheapest way to guarantee
// byte-identical output for identical input.
package canon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// ----------------------------------------- Public API ----------------------------------------- //

// Sort returns a deep copy of value in which every map is replaced by an ordered
// representation and every slice keeps its original order.
func Sort(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		ordered := &Ordered{Keys: keys, Values: make(map[string]any, len(typed))}
		for _, key := range keys {
			ordered.Values[key] = Sort(typed[key])
		}
		return ordered
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = Sort(item)
		}
		return out
	default:
		return value
	}
}

// Encode marshals value with sorted keys and the given indentation, ending with a
// newline. Every JSON file benchbook writes goes through this function, which is why
// two identical inputs produce byte-identical output.
func Encode(value any, indent string) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal value: %w", err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode value for canonicalisation: %w", err)
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetIndent("", indent)
	if err := encoder.Encode(Sort(document)); err != nil {
		return nil, fmt.Errorf("encode value: %w", err)
	}
	return buffer.Bytes(), nil
}

// Marshal returns the compact canonical encoding of value, used for hashing.
func Marshal(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal value: %w", err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode value for canonicalisation: %w", err)
	}
	var buffer bytes.Buffer
	if err := writeCanonical(&buffer, Sort(document)); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// marshalValue encodes one already-normalised value as JSON.
func marshalValue(value any) ([]byte, error) {
	return json.Marshal(value)
}

// marshalString encodes a single JSON string, delegating to encoding/json so
// escaping rules (including invalid UTF-8) stay exactly standard.
func marshalString(value string) ([]byte, error) {
	return json.Marshal(value)
}

// ---------------------------------------------- Types ----------------------------------------- //

// Ordered is a JSON object with an explicit key order. It satisfies
// json.Marshaler, which is what makes canonical output independent of Go's map
// iteration order.
type Ordered struct {
	// Keys are the object keys in the order they must be written.
	Keys []string
	// Values holds the (already sorted) value for each key.
	Values map[string]any
}

// MarshalJSON writes the object with its keys in the recorded order.
func (o *Ordered) MarshalJSON() ([]byte, error) {
	buffer := make([]byte, 0, 64)
	buffer = append(buffer, '{')
	for i, key := range o.Keys {
		if i > 0 {
			buffer = append(buffer, ',')
		}
		encodedKey, err := marshalString(key)
		if err != nil {
			return nil, err
		}
		buffer = append(buffer, encodedKey...)
		buffer = append(buffer, ':')
		encodedValue, err := marshalValue(o.Values[key])
		if err != nil {
			return nil, err
		}
		buffer = append(buffer, encodedValue...)
	}
	return append(buffer, '}'), nil
}

// writeCanonical writes the compact canonical encoding of value to the writer.
func writeCanonical(writer io.Writer, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal canonical payload: %w", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return fmt.Errorf("compact canonical payload: %w", err)
	}
	if _, err := writer.Write(compact.Bytes()); err != nil {
		return fmt.Errorf("write canonical payload: %w", err)
	}
	return nil
}
