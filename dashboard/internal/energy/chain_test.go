package energy

import (
	"reflect"
	"testing"
)

func TestApplyChainChange(t *testing.T) {
	type want struct {
		assignments map[string]Assignment
		roleNeeded  []string
	}
	cases := []struct {
		name        string
		assignments map[string]Assignment
		effective   map[string]Assignment
		prev        *Chain
		next        *Chain
		want        want
	}{
		{
			name:      "create takes the counting point's role",
			effective: map[string]Assignment{"plug": {Role: RoleLoad}, "ac": {Role: RoleBattery}},
			next:      &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{"plug": {Role: RoleLoad}, "ac": {}},
			},
		},
		{
			name:      "create without counting role takes the first role along the points",
			effective: map[string]Assignment{"ac": {Role: RoleBattery}},
			next:      &Chain{Points: []string{"plug", "ac", "dc"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{"plug": {Role: RoleBattery}, "ac": {}, "dc": {}},
			},
		},
		{
			name:        "create without any role stays without role",
			assignments: map[string]Assignment{"plug": {}, "ac": {}},
			effective:   map[string]Assignment{},
			next:        &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{"plug": {}, "ac": {}},
			},
		},
		{
			name: "counting change moves only the role",
			assignments: map[string]Assignment{
				"plug": {Role: RoleBattery, Scale: 1},
				"ac":   {Scale: 2, Invert: true},
			},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery, Scale: 1},
				"ac":   {Scale: 2, Invert: true},
			},
			prev: &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			next: &Chain{Points: []string{"plug", "ac"}, Counting: "ac"},
			want: want{
				assignments: map[string]Assignment{
					"ac":   {Role: RoleBattery, Scale: 2, Invert: true},
					"plug": {Scale: 1},
				},
			},
		},
		{
			name: "removing a reference point leaves it without role and asks for one",
			assignments: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"dc":   {Role: RolePV},
			},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"dc":   {Role: RolePV},
			},
			prev: &Chain{Points: []string{"plug", "ac", "dc"}, Counting: "plug"},
			next: &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{
					"plug": {Role: RoleBattery},
					"ac":   {},
					"dc":   {},
				},
				roleNeeded: []string{"dc"},
			},
		},
		{
			name: "removing the counting point hands its role on",
			assignments: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"dc":   {},
			},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"dc":   {},
			},
			prev: &Chain{Points: []string{"plug", "ac", "dc"}, Counting: "plug"},
			next: &Chain{Points: []string{"ac", "dc"}, Counting: "ac"},
			want: want{
				assignments: map[string]Assignment{
					"ac":   {Role: RoleBattery},
					"dc":   {},
					"plug": {},
				},
				roleNeeded: []string{"plug"},
			},
		},
		{
			name: "deleting keeps the counting role",
			assignments: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
			},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
			},
			prev: &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{
					"plug": {Role: RoleBattery},
					"ac":   {},
				},
				roleNeeded: []string{"ac"},
			},
		},
		{
			name:        "an added point with a role does not replace the chain role",
			assignments: map[string]Assignment{"plug": {Role: RoleBattery}, "ac": {}},
			effective: map[string]Assignment{
				"plug": {Role: RoleBattery},
				"ac":   {},
				"neu":  {Role: RoleLoad},
			},
			prev: &Chain{Points: []string{"plug", "ac"}, Counting: "plug"},
			next: &Chain{Points: []string{"plug", "ac", "neu"}, Counting: "plug"},
			want: want{
				assignments: map[string]Assignment{
					"plug": {Role: RoleBattery},
					"ac":   {},
					"neu":  {},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var input map[string]Assignment
			if tc.assignments != nil {
				input = make(map[string]Assignment, len(tc.assignments))
				for k, v := range tc.assignments {
					input[k] = v
				}
			}
			got, roleNeeded := ApplyChainChange(tc.assignments, tc.effective, tc.prev, tc.next)
			if !reflect.DeepEqual(got, tc.want.assignments) {
				t.Fatalf("assignments = %+v, want %+v", got, tc.want.assignments)
			}
			if len(roleNeeded) != len(tc.want.roleNeeded) || (len(roleNeeded) > 0 && !reflect.DeepEqual(roleNeeded, tc.want.roleNeeded)) {
				t.Fatalf("roleNeeded = %v, want %v", roleNeeded, tc.want.roleNeeded)
			}
			if !reflect.DeepEqual(tc.assignments, input) {
				t.Fatalf("input map was modified: %+v, want %+v", tc.assignments, input)
			}
		})
	}
}
