package settings

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/uierror"
)

func TestFormValidationErrorsCarryCatalogKeys(t *testing.T) {
	data, err := os.ReadFile("../webui/catalogs/de.json")
	if err != nil {
		t.Fatal(err)
	}
	de := map[string]string{}
	if err := json.Unmarshal(data, &de); err != nil {
		t.Fatal(err)
	}
	placeholder := regexp.MustCompile(`\{(\w+)\}`)
	cases := map[string]error{
		"host":         validateMQTT(MQTTConfig{Host: "bad host", Port: 1883, ClientID: "energy-node-dashboard", DiscoveryPrefix: "homeassistant", KeepaliveSeconds: 30, ConnectTimeoutSec: 10}),
		"prefix_empty": validateDiscoveryPrefix(""),
		"prefix_slash": validateDiscoveryPrefix("/ha"),
		"bridge_topic": validateBridgeTopic(BridgeTopic{Pattern: "#", Direction: "in"}),
		"bridge_qos":   validateBridgeTopic(BridgeTopic{Pattern: "a/b", Direction: "in", QoS: 3}),
		"bridge_max":   validateBridge(BridgeConfig{Connections: []BridgeConnection{{Name: "a", Address: "host", RemoteClientID: "id", Port: 1883, Topics: []BridgeTopic{{Pattern: "a", Direction: "in", QoS: 0}}, RestartTimeout: 30, KeepaliveSeconds: 60}, {Name: "b", Address: "host", RemoteClientID: "id", Port: 1883, Topics: []BridgeTopic{{Pattern: "a", Direction: "in", QoS: 0}}, RestartTimeout: 30, KeepaliveSeconds: 60}}}),
	}
	for name, mutate := range invalidEnergyCases() {
		value := validEnergyWithModel()
		mutate(&value)
		cases["energy_"+name] = validateEnergy(normalizeEnergy(value))
	}
	for name, err := range cases {
		typed, ok := uierror.From(err)
		if !ok {
			t.Errorf("%s: %v is not a uierror", name, err)
			continue
		}
		text, found := de[typed.Key]
		if !found {
			t.Errorf("%s: %s missing in de.json", name, typed.Key)
			continue
		}
		for _, match := range placeholder.FindAllStringSubmatch(text, -1) {
			if _, ok := typed.Params[match[1]]; !ok {
				t.Errorf("%s: %s uses {%s}, params %v lack it", name, typed.Key, match[1], typed.Params)
			}
		}
	}
}
