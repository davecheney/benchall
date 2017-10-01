// Copyright 2015 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package environschema_test

import (
	"github.com/juju/schema"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"gopkg.in/juju/environschema.v1"
)

type suite struct{}

var _ = gc.Suite(&suite{})

type valueTest struct {
	about       string
	val         interface{}
	expectError string
	expectVal   interface{}
}

var validationSchemaTests = []struct {
	about       string
	fields      environschema.Fields
	expectError string
	tests       []valueTest
}{{
	about: "regular schema",
	fields: environschema.Fields{
		"stringvalue": {
			Type: environschema.Tstring,
		},
		"mandatory-stringvalue": {
			Type:      environschema.Tstring,
			Mandatory: true,
		},
		"intvalue": {
			Type: environschema.Tint,
		},
		"boolvalue": {
			Type: environschema.Tbool,
		},
		"attrvalue": {
			Type: environschema.Tattrs,
		},
	},
	tests: []valueTest{{
		about: "all fields ok",
		val: map[string]interface{}{
			"stringvalue":           "hello",
			"mandatory-stringvalue": "goodbye",
			"intvalue":              320.0,
			"boolvalue":             true,
			"attrvalue":             "a=b c=d",
		},
		expectVal: map[string]interface{}{
			"stringvalue":           "hello",
			"intvalue":              320,
			"mandatory-stringvalue": "goodbye",
			"boolvalue":             true,
			"attrvalue":             map[string]string{"a": "b", "c": "d"},
		},
	}, {
		about: "non-mandatory fields missing",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
		},
		expectVal: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
		},
	}, {
		about: "wrong type for string",
		val: map[string]interface{}{
			"stringvalue":           123,
			"mandatory-stringvalue": "goodbye",
			"intvalue":              0,
			"boolvalue":             false,
		},
		expectError: `stringvalue: expected string, got int\(123\)`,
	}, {
		about: "int value specified as string",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"intvalue":              "100",
		},
		expectVal: map[string]interface{}{
			"intvalue":              100,
			"mandatory-stringvalue": "goodbye",
		},
	}, {
		about: "wrong type for int value",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"intvalue":              false,
		},
		expectError: `intvalue: expected number, got bool\(false\)`,
	}, {
		about: "attr type specified as list",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"attrvalue":             []interface{}{"a=b", "c=d"},
		},
		expectVal: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"attrvalue":             map[string]string{"a": "b", "c": "d"},
		},
	}, {
		about: "attr type specified as map",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"attrvalue":             map[interface{}]interface{}{"a": "b", "c": "d"},
		},
		expectVal: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"attrvalue":             map[string]string{"a": "b", "c": "d"},
		},
	}, {
		about: "invalid attrs string value",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"attrvalue":             "a=b d f=gh",
		},
		expectError: `attrvalue: expected "key=value", got "d"`,
	}, {
		about: "invalid attrs list value",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"attrvalue":             []interface{}{"a=b d", "f"},
		},
		expectError: `attrvalue: expected "key=value", got "f"`,
	}, {
		about: "attrs list element not coercable",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"attrvalue":             []interface{}{"a=b d", 123.45},
		},
		expectError: `attrvalue\[1\]: expected string, got float64\(123\.45\)`,
	}, {
		about: "attrs map element not coercable",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"attrvalue":             map[interface{}]interface{}{"a": 123, "c": "d"},
		},
		expectError: `attrvalue\.a: expected string, got int\(123\)`,
	}, {
		about: "unexpected attrs type",
		val: map[string]interface{}{
			"mandatory-stringvalue": "goodbye",
			"attrvalue":             123.45,
		},
		expectError: `attrvalue: unexpected type for value, got float64\(123\.45\)`,
	}},
}, {
	about: "enumerated values",
	fields: environschema.Fields{
		"enumstring": {
			Type:   environschema.Tstring,
			Values: []interface{}{"a", "b"},
		},
		"enumint": {
			Type:   environschema.Tint,
			Values: []interface{}{10, "20"},
		},
	},
	tests: []valueTest{{
		about: "all fields ok",
		val: map[string]interface{}{
			"enumstring": "a",
			"enumint":    20,
		},
		expectVal: map[string]interface{}{
			"enumstring": "a",
			"enumint":    20,
		},
	}, {
		about: "string value not in values",
		val: map[string]interface{}{
			"enumstring": "wrong",
			"enumint":    20,
		},
		expectError: `enumstring: expected one of \[a b\], got "wrong"`,
	}, {
		about: "int value not in values",
		val: map[string]interface{}{
			"enumstring": "b",
			"enumint":    "5",
		},
		expectError: `enumint: expected one of \[10 20\], got 5`,
	}, {
		about: "invalid type for string value",
		val: map[string]interface{}{
			"enumstring": 123,
			"enumint":    10,
		},
		expectError: `enumstring: expected string, got int\(123\)`,
	}, {
		about: "invalid type for int value",
		val: map[string]interface{}{
			"enumstring": "b",
			"enumint":    false,
		},
		expectError: `enumint: expected number, got bool\(false\)`,
	}},
}, {
	about: "invalid value type",
	fields: environschema.Fields{
		"stringvalue": {
			Type: "nontype",
		},
	},
	expectError: `stringvalue: invalid type "nontype"`,
}}

func (*suite) TestValidationSchema(c *gc.C) {
	for i, test := range validationSchemaTests {
		c.Logf("test %d: %s", i, test.about)
		sfields, sdefaults, err := test.fields.ValidationSchema()
		if test.expectError != "" {
			c.Assert(err, gc.ErrorMatches, test.expectError)
			continue
		}
		c.Assert(err, gc.IsNil)
		checker := schema.FieldMap(sfields, sdefaults)
		for j, vtest := range test.tests {
			c.Logf("- test %d: %s", j, vtest.about)
			val, err := checker.Coerce(vtest.val, nil)
			if vtest.expectError != "" {
				c.Assert(err, gc.ErrorMatches, vtest.expectError)
				continue
			}
			c.Assert(err, gc.IsNil)
			c.Assert(val, jc.DeepEquals, vtest.expectVal)
		}
	}
}
