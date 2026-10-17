package sfs

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
)

// JSONScalar converts an explicit {"$sfs":"int","value":1} scalar wrapper.
// Errors deliberately contain no input values, which may be credentials.
func JSONScalar(raw map[string]any) (SFSValue, error) {
	bad := errors.New("invalid typed SFS scalar")
	if len(raw) != 2 {
		return SFSValue{}, bad
	}
	kind, ok := raw["$sfs"].(string)
	if !ok {
		return SFSValue{}, bad
	}
	value, ok := raw["value"]
	if !ok {
		return SFSValue{}, bad
	}
	switch kind {
	case "utf":
		v, ok := value.(string)
		if ok {
			return SFSValue{SFSUtfString, v}, nil
		}
	case "bool":
		v, ok := value.(bool)
		if ok {
			return SFSValue{SFSBool, v}, nil
		}
	case "byte", "short", "int", "long":
		n, ok := value.(json.Number)
		if !ok {
			return SFSValue{}, bad
		}
		v, err := strconv.ParseInt(n.String(), 10, 64)
		if err != nil {
			return SFSValue{}, bad
		}
		switch kind {
		case "byte":
			if v >= 0 && v <= 255 {
				return SFSValue{sfsByte, byte(v)}, nil
			}
		case "short":
			if v >= math.MinInt16 && v <= math.MaxInt16 {
				return SFSValue{sfsShort, int16(v)}, nil
			}
		case "int":
			if v >= math.MinInt32 && v <= math.MaxInt32 {
				return SFSValue{SFSInt, int32(v)}, nil
			}
		case "long":
			return SFSValue{SFSLong, v}, nil
		}
	case "double":
		n, ok := value.(json.Number)
		if !ok {
			return SFSValue{}, bad
		}
		v, err := n.Float64()
		if err == nil && !math.IsInf(v, 0) && !math.IsNaN(v) {
			return SFSValue{SFSDouble, v}, nil
		}
	}
	return SFSValue{}, bad
}
