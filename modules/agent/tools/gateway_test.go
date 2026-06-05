package tools

import "testing"

func gw(caps ...string) *ToolGateway {
	g := &ToolGateway{BaseURL: "https://gw.example", Key: "GWKEY", caps: map[string]bool{}}
	for _, c := range caps {
		g.caps[c] = true
	}
	return g
}

func TestGatewayRoutesCoveredCapability(t *testing.T) {
	g := gw(CapSearch, CapImage)
	url, key, via := g.Endpoint(CapSearch, "https://vendor", "")
	if !via || url != "https://gw.example" || key != "GWKEY" {
		t.Fatalf("covered capability should route to gateway: url=%q key=%q via=%v", url, key, via)
	}
}

func TestGatewayPerToolKeyWins(t *testing.T) {
	g := gw(CapSearch)
	url, key, via := g.Endpoint(CapSearch, "https://vendor", "VENDORKEY")
	if via || url != "https://vendor" || key != "VENDORKEY" {
		t.Fatalf("explicit per-tool key must win over the gateway: url=%q key=%q via=%v", url, key, via)
	}
}

func TestGatewayUncoveredCapabilityFallsThrough(t *testing.T) {
	g := gw(CapImage) // does not cover search
	url, _, via := g.Endpoint(CapSearch, "https://vendor", "")
	if via || url != "https://vendor" {
		t.Fatalf("uncovered capability should use the vendor: url=%q via=%v", url, via)
	}
}

func TestGatewayNilIsPassthrough(t *testing.T) {
	var g *ToolGateway // no gateway configured
	url, key, via := g.Endpoint(CapSearch, "https://vendor", "vk")
	if via || url != "https://vendor" || key != "vk" {
		t.Fatalf("nil gateway should pass through unchanged: %q %q %v", url, key, via)
	}
	// And with no vendor key either.
	url, key, via = g.Endpoint(CapSearch, "https://vendor", "")
	if via || url != "https://vendor" || key != "" {
		t.Fatalf("nil gateway, no key: %q %q %v", url, key, via)
	}
}

func TestGatewayHas(t *testing.T) {
	g := gw(CapTTS)
	if !g.Has(CapTTS) || g.Has(CapSearch) {
		t.Fatal("Has() wrong")
	}
	var nilg *ToolGateway
	if nilg.Has(CapTTS) {
		t.Fatal("nil gateway covers nothing")
	}
}
