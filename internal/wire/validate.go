package wire

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/swiftkimani/ivr-voice-gateway/schema"
)

// Validate checks one JSON message against the v1 contract.
//
// It covers the subset of JSON Schema the contract uses — a oneOf of closed
// object definitions, each picked by its `type` constant — rather than pulling
// in a general validator for a schema we control. Anything the contract starts
// using that is not handled here fails loudly in schemaDef, not silently.
func Validate(raw []byte) error {
	s, err := contract()
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("wire: not an object: %w", err)
	}
	kind, _ := obj["type"].(string)
	if kind == "" {
		return fmt.Errorf("wire: message has no type")
	}
	def, ok := s.byType[kind]
	if !ok {
		return fmt.Errorf("wire: %q is not a message type in v1", kind)
	}
	return s.object(def, obj, kind)
}

// MessageTypes lists every message type the contract defines, in schema order.
func MessageTypes() ([]string, error) {
	s, err := contract()
	if err != nil {
		return nil, err
	}
	return s.order, nil
}

// Properties returns the property names the contract declares for one message
// type. Tests use it to prove the fixtures exercise the whole contract.
func Properties(kind string) ([]string, error) {
	s, err := contract()
	if err != nil {
		return nil, err
	}
	def, ok := s.byType[kind]
	if !ok {
		return nil, fmt.Errorf("wire: %q is not a message type in v1", kind)
	}
	names := make([]string, 0, len(def.Properties))
	for name := range def.Properties {
		names = append(names, name)
	}
	return names, nil
}

// def is one JSON Schema node, limited to the keywords the contract uses.
type def struct {
	Ref                  string          `json:"$ref"`
	Type                 string          `json:"type"`
	Const                any             `json:"const"`
	Enum                 []any           `json:"enum"`
	Pattern              string          `json:"pattern"`
	Format               string          `json:"format"`
	Minimum              *float64        `json:"minimum"`
	MinLength            *int            `json:"minLength"`
	MaxLength            *int            `json:"maxLength"`
	Required             []string        `json:"required"`
	Properties           map[string]*def `json:"properties"`
	AdditionalProperties *bool           `json:"additionalProperties"`
	Items                *def            `json:"items"`
	Description          string          `json:"description"`
	Default              any             `json:"default"`
}

type contractSchema struct {
	OneOf []struct {
		Ref string `json:"$ref"`
	} `json:"oneOf"`
	Defs map[string]*def `json:"$defs"`

	byType map[string]*def
	order  []string
}

var (
	loadOnce sync.Once
	loaded   *contractSchema
	loadErr  error
)

// contract parses the embedded schema once and indexes it by message type.
func contract() (*contractSchema, error) {
	loadOnce.Do(func() {
		var s contractSchema
		if err := json.Unmarshal(schema.V1, &s); err != nil {
			loadErr = fmt.Errorf("wire: parsing the embedded schema: %w", err)
			return
		}
		s.byType = make(map[string]*def, len(s.OneOf))
		for _, branch := range s.OneOf {
			d, err := s.resolve(&def{Ref: branch.Ref})
			if err != nil {
				loadErr = err
				return
			}
			t := d.Properties["type"]
			if t == nil || t.Const == nil {
				loadErr = fmt.Errorf("wire: schema branch %s has no type constant", branch.Ref)
				return
			}
			name, ok := t.Const.(string)
			if !ok {
				loadErr = fmt.Errorf("wire: schema branch %s has a non-string type constant", branch.Ref)
				return
			}
			s.byType[name] = d
			s.order = append(s.order, name)
		}
		loaded = &s
	})
	return loaded, loadErr
}

// resolve follows a local $ref, returning the node itself when there is none.
func (s *contractSchema) resolve(d *def) (*def, error) {
	if d.Ref == "" {
		return d, nil
	}
	name := strings.TrimPrefix(d.Ref, "#/$defs/")
	if name == d.Ref {
		return nil, fmt.Errorf("wire: unsupported $ref %q", d.Ref)
	}
	target, ok := s.Defs[name]
	if !ok {
		return nil, fmt.Errorf("wire: $ref %q does not resolve", d.Ref)
	}
	return target, nil
}

// object validates a whole message against its definition.
func (s *contractSchema) object(d *def, obj map[string]any, kind string) error {
	for _, name := range d.Required {
		if _, ok := obj[name]; !ok {
			return fmt.Errorf("wire: %s is missing the required field %q", kind, name)
		}
	}
	closed := d.AdditionalProperties != nil && !*d.AdditionalProperties
	for name, v := range obj {
		prop, ok := d.Properties[name]
		if !ok {
			if closed {
				return fmt.Errorf("wire: %s carries %q, which v1 does not define", kind, name)
			}
			continue
		}
		resolved, err := s.resolve(prop)
		if err != nil {
			return err
		}
		if err := s.value(resolved, v); err != nil {
			return fmt.Errorf("wire: %s field %q: %w", kind, name, err)
		}
	}
	return nil
}

// value validates one field against its definition.
func (s *contractSchema) value(d *def, v any) error {
	if d.Const != nil && v != d.Const {
		return fmt.Errorf("must be %v, got %v", d.Const, v)
	}
	if len(d.Enum) > 0 && !contains(d.Enum, v) {
		return fmt.Errorf("%v is not one of %v", v, d.Enum)
	}
	switch d.Type {
	case "string":
		return s.stringValue(d, v)
	case "integer":
		n, ok := v.(float64)
		if !ok {
			return fmt.Errorf("must be an integer, got %T", v)
		}
		if n != math.Trunc(n) {
			return fmt.Errorf("must be a whole number, got %v", n)
		}
		if d.Minimum != nil && n < *d.Minimum {
			return fmt.Errorf("must be at least %v, got %v", *d.Minimum, n)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("must be a boolean, got %T", v)
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return fmt.Errorf("must be an object, got %T", v)
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return fmt.Errorf("must be an array, got %T", v)
		}
	case "":
		// No type constraint, e.g. a node carrying only a const or enum.
	default:
		return fmt.Errorf("unsupported schema type %q; teach wire.Validate about it", d.Type)
	}
	return nil
}

func (s *contractSchema) stringValue(d *def, v any) error {
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string, got %T", v)
	}
	if d.MinLength != nil && len(str) < *d.MinLength {
		return fmt.Errorf("must be at least %d characters", *d.MinLength)
	}
	if d.MaxLength != nil && len(str) > *d.MaxLength {
		return fmt.Errorf("must be at most %d characters", *d.MaxLength)
	}
	if d.Pattern != "" {
		re, err := regexp.Compile(d.Pattern)
		if err != nil {
			return fmt.Errorf("schema pattern %q does not compile: %w", d.Pattern, err)
		}
		if !re.MatchString(str) {
			return fmt.Errorf("%q does not match %s", str, d.Pattern)
		}
	}
	// Timestamps order the event log and drive replay, so a malformed one is
	// worth catching at the boundary rather than on the way out.
	if d.Format == "date-time" {
		if _, err := time.Parse(time.RFC3339, str); err != nil {
			return fmt.Errorf("%q is not an RFC 3339 timestamp", str)
		}
	}
	return nil
}

func contains(list []any, v any) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
