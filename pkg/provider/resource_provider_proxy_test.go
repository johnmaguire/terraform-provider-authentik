package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccResourceProviderProxy(t *testing.T) {
	rName := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	appName := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	resource.UnitTest(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceProviderProxy(rName, appName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("authentik_provider_proxy.name", "name", rName),
					resource.TestCheckResourceAttr("authentik_provider_proxy.name", "external_host", "http://"+rName),
					resource.TestCheckResourceAttr("authentik_application.name", "name", appName),
					resource.TestCheckResourceAttr("authentik_application.name", "slug", appName),
				),
			},
			{
				Config: testAccResourceProviderProxy(rName+"test", appName+"test"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("authentik_provider_proxy.name", "name", rName+"test"),
					resource.TestCheckResourceAttr("authentik_provider_proxy.name", "external_host", "http://"+rName+"test"),
					resource.TestCheckResourceAttr("authentik_application.name", "name", appName+"test"),
					resource.TestCheckResourceAttr("authentik_application.name", "slug", appName+"test"),
				),
			},
		},
	})
}

func testAccResourceProviderProxy(name string, appName string) string {
	return fmt.Sprintf(`
data "authentik_flow" "default-authorization-flow" {
  slug = "default-provider-authorization-implicit-consent"
}

data "authentik_flow" "default-provider-invalidation-flow" {
  slug = "default-provider-invalidation-flow"
}

data "authentik_property_mapping_provider_scope" "test" {
  managed_list = [
    "goauthentik.io/providers/oauth2/scope-openid",
    "goauthentik.io/providers/oauth2/scope-email",
    "goauthentik.io/providers/oauth2/scope-profile",
    "goauthentik.io/providers/oauth2/scope-entitlements",
    "goauthentik.io/providers/proxy/scope-proxy",
  ]
}

resource "authentik_provider_proxy" "name" {
  name      = "%[1]s"
  internal_host = "http://foo.bar.baz"
  external_host = "http://%[1]s"
  authorization_flow = data.authentik_flow.default-authorization-flow.id
  invalidation_flow = data.authentik_flow.default-provider-invalidation-flow.id
  property_mappings = data.authentik_property_mapping_provider_scope.test.ids
  skip_path_regex    = <<EOF
^/$
^/status
^/assets/
^/assets
^/icon.svg
^/api/.*
^/upload/.*
^/metrics
EOF
}

resource "authentik_application" "name" {
  name              = "%[2]s"
  slug              = "%[2]s"
  protocol_provider = authentik_provider_proxy.name.id
}
`, name, appName)
}

func TestAccResourceProviderProxyMappings(t *testing.T) {
	name := acctest.RandStringFromCharSet(10, acctest.CharSetAlphaNum)
	resource.UnitTest(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceProviderProxyMappings(name, "[authentik_property_mapping_provider_scope.first.id]"),
				Check:  resource.TestCheckResourceAttr("authentik_provider_proxy.test", "property_mappings.#", "1"),
			},
			{
				Config: testAccResourceProviderProxyMappings(name, "[authentik_property_mapping_provider_scope.first.id, authentik_property_mapping_provider_scope.second.id]"),
				Check:  resource.TestCheckResourceAttr("authentik_provider_proxy.test", "property_mappings.#", "2"),
			},
			{
				Config: testAccResourceProviderProxyMappings(name, "[authentik_property_mapping_provider_scope.second.id]"),
				Check:  resource.TestCheckResourceAttr("authentik_provider_proxy.test", "property_mappings.#", "1"),
			},
			{
				Config: testAccResourceProviderProxyMappings(name, "[]"),
				Check:  resource.TestCheckResourceAttr("authentik_provider_proxy.test", "property_mappings.#", "0"),
			},
			{
				Config: testAccResourceProviderProxyMappings(name, "concat(data.authentik_property_mapping_provider_scope.defaults.ids, [authentik_property_mapping_provider_scope.first.id])"),
				Check:  resource.TestCheckResourceAttr("authentik_provider_proxy.test", "property_mappings.#", "6"),
			},
			{
				Config: testAccResourceProviderProxyMappings(name, "[authentik_property_mapping_provider_scope.first.id]"),
				Check:  resource.TestCheckResourceAttr("authentik_provider_proxy.test", "property_mappings.#", "1"),
			},
			{
				ResourceName:      "authentik_provider_proxy.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccResourceProviderProxyMappings(name, mappings string) string {
	return fmt.Sprintf(`
data "authentik_flow" "authorization" {
  slug = "default-provider-authorization-implicit-consent"
}

data "authentik_flow" "invalidation" {
  slug = "default-provider-invalidation-flow"
}

data "authentik_property_mapping_provider_scope" "defaults" {
  managed_list = [
    "goauthentik.io/providers/oauth2/scope-openid",
    "goauthentik.io/providers/oauth2/scope-profile",
    "goauthentik.io/providers/oauth2/scope-email",
    "goauthentik.io/providers/oauth2/scope-entitlements",
    "goauthentik.io/providers/proxy/scope-proxy",
  ]
}

resource "authentik_property_mapping_provider_scope" "first" {
  name       = "%[1]s-first"
  scope_name = "first"
  expression = "return {}"
}

resource "authentik_property_mapping_provider_scope" "second" {
  name       = "%[1]s-second"
  scope_name = "second"
  expression = "return {}"
}

resource "authentik_provider_proxy" "test" {
  name               = "%[1]s"
  mode               = "forward_single"
  external_host      = "https://%[1]s.example.test"
  authorization_flow = data.authentik_flow.authorization.id
  invalidation_flow  = data.authentik_flow.invalidation.id
  property_mappings  = %[2]s
}
`, name, mappings)
}
