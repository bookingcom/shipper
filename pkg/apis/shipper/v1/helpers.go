package v1

import (
	encodingjson "encoding/json"
	"fmt"
)

// these functions are imported from apimachinery/pkg/runtime/converter.go in
// order to make it tolerate uint64s, which evidently are what raw YAML numbers
// show up as

// deepCopyJSON deep copies the passed value, assuming it is a valid JSON representation i.e. only contains
// types produced by json.Unmarshal().
func deepCopyJSON(x map[string]interface{}) map[string]interface{} {
	// NOTE(isutton) We don't check the type assertion in here since we want it
	// to crash in runtime. That is, until someone raises the point during
	// code review :)
	return deepCopyJSONValue(x).(map[string]interface{})
}

// deepCopyJSONValue deep copies the passed value, assuming it is a valid JSON representation i.e. only contains
// types produced by json.Unmarshal().
func deepCopyJSONValue(x interface{}) interface{} {
	switch x := x.(type) {
	case map[string]interface{}:
		clone := make(map[string]interface{}, len(x))
		for k, v := range x {
			clone[k] = deepCopyJSONValue(v)
		}
		return clone
	case []interface{}:
		clone := make([]interface{}, len(x))
		for i, v := range x {
			clone[i] = deepCopyJSONValue(v)
		}
		return clone
	case string, int64, bool, float64, nil, encodingjson.Number:
		return x
	case uint64:
		// numbers in shipment orders shouldn't ever be so large that they overflow.
		return int64(x)
	default:
		panic(fmt.Errorf("cannot deep copy %T", x))
	}
}
