package rolloutblock

import (
	"reflect"
	"sort"
	"testing"
)

func TestString(t *testing.T) {
	dnsOutageString := "rollout-blocks-global/dns-outage"
	demoOutageString := "frontend/demo-to-investors-in-progress"
	tests := []struct {
		Name     string
		List     ObjectNameList
		Expected string
	}{
		{
			"one object name",
			NewObjectNameList(dnsOutageString),
			dnsOutageString,
		},
		{
			"two object names",
			NewObjectNameList(dnsOutageString + "," + demoOutageString),
			demoOutageString + "," + dnsOutageString,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			listString := tt.List.String()
			if listString != tt.Expected {
				t.Errorf("Unexpected result returned by ObjectNameList.String(): got: %s, want: %s", listString, tt.Expected)
			}
		})
	}
}

func TestKeys(t *testing.T) {
	dnsOutageString := "rollout-blocks-global/dns-outage"
	demoOutageString := "frontend/demo-to-investors-in-progress"
	tests := []struct {
		Name     string
		List     ObjectNameList
		Expected []string
	}{
		{
			"one object name",
			NewObjectNameList(dnsOutageString),
			[]string{dnsOutageString},
		},
		{
			"two object names",
			NewObjectNameList(dnsOutageString + "," + demoOutageString),
			[]string{demoOutageString, dnsOutageString},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			listString := tt.List.Keys()
			sort.Strings(listString)
			if !reflect.DeepEqual(listString, tt.Expected) {
				t.Errorf("Unexpected result returned by ObjectNameList.Keys(): got: %s, want: %s", listString, tt.Expected)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	dnsOutageString := "rollout-blocks-global/dns-outage"
	demoOutageString := "frontend/demo-to-investors-in-progress"
	tests := []struct {
		Name          string
		List          ObjectNameList
		DeletedObject string
		Expected      ObjectNameList
	}{
		{
			"one object name list",
			NewObjectNameList(dnsOutageString),
			dnsOutageString,
			NewObjectNameList(""),
		},
		{
			"two object names",
			NewObjectNameList(dnsOutageString + "," + demoOutageString),
			dnsOutageString,
			NewObjectNameList(demoOutageString),
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.List.Delete(tt.DeletedObject) // delete in place
			if !reflect.DeepEqual(tt.List, tt.Expected) {
				t.Errorf("Unexpected result returned by ObjectNameList.Delete(): got: %s, want: %s", tt.List, tt.Expected)
			}
		})
	}
}

func TestAdd(t *testing.T) {
	dnsOutageString := "rollout-blocks-global/dns-outage"
	demoOutageString := "frontend/demo-to-investors-in-progress"
	tests := []struct {
		Name        string
		List        ObjectNameList
		AddedObject string
		Expected    ObjectNameList
	}{
		{
			"add one object to empty object name list",
			NewObjectNameList(""),
			demoOutageString,
			NewObjectNameList(demoOutageString),
		},
		{
			"one object name list to two",
			NewObjectNameList(dnsOutageString),
			demoOutageString,
			NewObjectNameList(demoOutageString + "," + dnsOutageString),
		},
		{
			"add existing object",
			NewObjectNameList(dnsOutageString + "," + demoOutageString),
			dnsOutageString,
			NewObjectNameList(dnsOutageString + "," + demoOutageString),
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.List.Add(tt.AddedObject) // adding in place
			if !reflect.DeepEqual(tt.List, tt.Expected) {
				t.Errorf("Unexpected result returned by ObjectNameList.Add(): got: %s, want: %s", tt.List, tt.Expected)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	dnsOutageString := "rollout-blocks-global/dns-outage"
	demoOutageString := "frontend/demo-to-investors-in-progress"
	tests := []struct {
		Name      string
		List      ObjectNameList
		OtherList ObjectNameList
		Expected  ObjectNameList
	}{
		{
			"no difference",
			NewObjectNameList(demoOutageString),
			NewObjectNameList(demoOutageString),
			NewObjectNameList(""),
		},
		{
			"one object differs",
			NewObjectNameList(demoOutageString + "," + dnsOutageString),
			NewObjectNameList(dnsOutageString),
			NewObjectNameList(demoOutageString),
		},
		{
			"two objects differ",
			NewObjectNameList(dnsOutageString + "," + demoOutageString),
			NewObjectNameList(""),
			NewObjectNameList(dnsOutageString + "," + demoOutageString),
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			diff := tt.List.Diff(tt.OtherList)
			if !reflect.DeepEqual(diff, tt.Expected) {
				t.Errorf("Unexpected result returned by ObjectNameList.Diff(): got: %s, want: %s", diff, tt.Expected)
			}
		})
	}
}
