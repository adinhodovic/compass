package meta

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// JSONItems resolves itemsPath against payload and returns the items
// according to mode:
//
//   - "" or "array": the resolved value must be an array.
//   - "values": the resolved value must be an object; its values become items.
//
// Shared by the api source and status providers so both read JSON
// endpoints with the same mapping semantics.
func JSONItems(payload []byte, itemsPath, mode string) ([]gjson.Result, error) {
	root := gjson.ParseBytes(payload)
	if path := JSONPath(itemsPath); path != "" {
		root = gjson.GetBytes(payload, path)
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "array":
		if !root.IsArray() {
			return nil, fmt.Errorf("items_path %q did not resolve to an array", itemsPath)
		}
		return root.Array(), nil
	case "values":
		if !root.IsObject() {
			return nil, fmt.Errorf(
				"items_path %q did not resolve to a map (items_mode=values)",
				itemsPath,
			)
		}
		obj := root.Map()
		items := make([]gjson.Result, 0, len(obj))
		for _, value := range obj {
			items = append(items, value)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unsupported items_mode %q", mode)
	}
}

// JSONPath normalizes a user-supplied gjson path, accepting an optional
// JSONPath-style "$." prefix. "$" alone means the root and maps to "".
func JSONPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$.")
	if path == "$" {
		return ""
	}
	return path
}
