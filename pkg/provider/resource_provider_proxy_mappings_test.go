package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	api "goauthentik.io/api/v3"
	"goauthentik.io/terraform-provider-authentik/pkg/helpers"
)

var testProxyDefaultMappings = []string{"openid", "profile", "email", "entitlements", "proxy"}

func TestResourceProviderProxyReadMappings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		local  []string
		remote []string
		want   []string
	}{
		{"defaults only", nil, nil, []string{}},
		{"custom only", []string{"custom"}, []string{"custom"}, []string{"custom"}},
		{"explicit defaults", append(slices.Clone(testProxyDefaultMappings), "custom"), []string{"custom"}, append(slices.Clone(testProxyDefaultMappings), "custom")},
		{"partial defaults", []string{"email", "custom"}, []string{"custom"}, []string{"email", "custom"}},
		{"import custom", nil, []string{"custom"}, []string{"custom"}},
		{"unexpected custom", []string{"custom"}, []string{"custom", "unexpected"}, []string{"custom", "unexpected"}},
		{"unexpected custom with empty state", nil, []string{"unexpected"}, []string{"unexpected"}},
		{"custom deleted outside Terraform", []string{"custom"}, nil, []string{}},
		{"custom API order changes", []string{"second", "first"}, []string{"first", "second"}, []string{"second", "first"}},
		{"managed non-default", nil, []string{"offline-access"}, []string{"offline-access"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, remote := testProxyMappingsAPI(t)
			remote.PropertyMappings = append(remote.PropertyMappings, tc.remote...)
			d := schema.TestResourceDataRaw(t, resourceProviderProxy().Schema, testProxyMappingsConfig(tc.local))
			d.SetId("1")
			diags := resourceProviderProxyRead(t.Context(), d, client)
			require.False(t, diags.HasError(), diags)
			assert.Equal(t, tc.want, helpers.CastSlice[string](d, "property_mappings"))
		})
	}
}

func TestResourceProviderProxyMappingsConverge(t *testing.T) {
	client, remote := testProxyMappingsAPI(t)
	r := resourceProviderProxy()
	d := schema.TestResourceDataRaw(t, r.Schema, testProxyMappingsConfig([]string{"first"}))
	require.False(t, resourceProviderProxyCreate(t.Context(), d, client).HasError())
	assert.ElementsMatch(t, append(slices.Clone(testProxyDefaultMappings), "first"), remote.PropertyMappings)
	// Existing state can include built-in mappings that were never declared in configuration.
	require.NoError(t, d.Set("property_mappings", remote.PropertyMappings))

	for _, mappings := range [][]string{{"first"}, {"second", "first"}, {"second"}, {}, {"email", "first"}, {}} {
		config := testProxyMappingsConfig(mappings)
		diff, err := r.SimpleDiff(t.Context(), d.State(), terraform.NewResourceConfigRaw(config), client)
		require.NoError(t, err)
		if !slices.Equal(helpers.CastSlice[string](d, "property_mappings"), mappings) {
			require.NotEmpty(t, diff.Attributes, "mapping membership changes must produce a diff")
		}
		require.NoError(t, d.Set("property_mappings", mappings))
		require.False(t, resourceProviderProxyUpdate(t.Context(), d, client).HasError())
		require.Equal(t, mappings, helpers.CastSlice[string](d, "property_mappings"))
		for _, id := range []string{"first", "second"} {
			assert.Equal(t, slices.Contains(mappings, id), slices.Contains(remote.PropertyMappings, id))
		}
		for range 2 {
			require.False(t, resourceProviderProxyRead(t.Context(), d, client).HasError())
			diff, err = r.SimpleDiff(t.Context(), d.State(), terraform.NewResourceConfigRaw(config), client)
			require.NoError(t, err)
			assert.Empty(t, diff.Attributes, "unchanged mappings must converge")
		}
	}
}

func TestResourceProviderProxyImportPreservesCustomMappings(t *testing.T) {
	client, remote := testProxyMappingsAPI(t)
	remote.PropertyMappings = append(remote.PropertyMappings, "custom", "offline-access")
	r := resourceProviderProxy()
	d := schema.TestResourceDataRaw(t, r.Schema, nil)
	d.SetId("1")
	imported, err := r.Importer.StateContext(t.Context(), d, client)
	require.NoError(t, err)
	require.Len(t, imported, 1)
	d = imported[0]
	require.False(t, resourceProviderProxyRead(t.Context(), d, client).HasError())
	require.Equal(t, []string{"custom", "offline-access"}, helpers.CastSlice[string](d, "property_mappings"))
	require.NoError(t, d.Set("name", "renamed"))
	require.False(t, resourceProviderProxyUpdate(t.Context(), d, client).HasError())
	assert.ElementsMatch(t, append(slices.Clone(testProxyDefaultMappings), "custom", "offline-access"), remote.PropertyMappings)
}

func TestResourceProviderProxyMappingsDetectDrift(t *testing.T) {
	client, remote := testProxyMappingsAPI(t)
	r := resourceProviderProxy()
	config := testProxyMappingsConfig(nil)
	d := schema.TestResourceDataRaw(t, r.Schema, config)
	d.SetId("1")
	remote.PropertyMappings = append(remote.PropertyMappings, "unexpected", "offline-access")
	require.False(t, resourceProviderProxyRead(t.Context(), d, client).HasError())
	diff, err := r.SimpleDiff(t.Context(), d.State(), terraform.NewResourceConfigRaw(config), client)
	require.NoError(t, err)
	require.Contains(t, diff.Attributes, "property_mappings.#")
	assert.Equal(t, "2", diff.Attributes["property_mappings.#"].Old)
	assert.Equal(t, "0", diff.Attributes["property_mappings.#"].New)
}

func TestResourceProviderProxyMappingsReadError(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, _ := testProxyMappingsAPI(t)
			client.client.GetConfig().HTTPClient.Transport = proxyMappingsErrorTransport{base: http.DefaultTransport, status: status}
			d := schema.TestResourceDataRaw(t, resourceProviderProxy().Schema, testProxyMappingsConfig([]string{"custom"}))
			d.SetId("1")
			diags := resourceProviderProxyRead(t.Context(), d, client)
			require.True(t, diags.HasError(), diags)
			assert.Equal(t, "1", d.Id(), "mapping lookup errors must not remove the provider from state")
			assert.Equal(t, []string{"custom"}, helpers.CastSlice[string](d, "property_mappings"))
		})
	}
}

type proxyMappingsErrorTransport struct {
	base   http.RoundTripper
	status int
}

func (tr proxyMappingsErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.Contains(r.URL.Path, "/propertymappings/") {
		return &http.Response{
			StatusCode: tr.status,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       http.NoBody,
			Request:    r,
		}, nil
	}
	return tr.base.RoundTrip(r)
}

func testProxyMappingsConfig(mappings []string) map[string]any {
	values := make([]any, len(mappings))
	for i, id := range mappings {
		values[i] = id
	}
	return map[string]any{
		"name":               "proxy",
		"authorization_flow": "authorization",
		"invalidation_flow":  "invalidation",
		"external_host":      "https://example.test",
		"mode":               "forward_single",
		"property_mappings":  values,
	}
}

func testProxyMappingsAPI(t *testing.T) (*APIClient, *api.ProxyProvider) {
	t.Helper()
	remote := &api.ProxyProvider{
		Pk:                        1,
		Name:                      "proxy",
		AuthorizationFlow:         "authorization",
		InvalidationFlow:          "invalidation",
		ExternalHost:              "https://example.test",
		ClientId:                  "client",
		Mode:                      api.PROXYMODE_FORWARD_SINGLE.Ptr(),
		InternalHostSslValidation: new(true),
		InterceptHeaderAuth:       new(true),
		BasicAuthEnabled:          new(false),
		AccessTokenValidity:       new("minutes=10"),
		RefreshTokenValidity:      new("days=30"),
		PropertyMappings:          slices.Clone(testProxyDefaultMappings),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/api/v3/propertymappings/provider/scope/" {
			assert.Equal(t, http.MethodGet, req.Method)
			managed := []string{
				"goauthentik.io/providers/oauth2/scope-openid",
				"goauthentik.io/providers/oauth2/scope-profile",
				"goauthentik.io/providers/oauth2/scope-email",
				"goauthentik.io/providers/oauth2/scope-entitlements",
				"goauthentik.io/providers/proxy/scope-proxy",
			}
			assert.ElementsMatch(t, managed, req.URL.Query()["managed"])
			results := make([]api.ScopeMapping, len(managed))
			for i, id := range managed {
				results[i] = api.ScopeMapping{Pk: testProxyDefaultMappings[i], Managed: *api.NewNullableString(&id)}
			}
			assert.NoError(t, json.NewEncoder(w).Encode(api.PaginatedScopeMappingList{Results: results}))
			return
		}
		if req.URL.Path != "/api/v3/providers/proxy/1/" && req.URL.Path != "/api/v3/providers/proxy/" {
			t.Errorf("unexpected API request: %s %s", req.Method, req.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if req.Method == http.MethodPost || req.Method == http.MethodPut {
			var body api.ProxyProviderRequest
			if !assert.NoError(t, json.NewDecoder(req.Body).Decode(&body)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.NotNil(t, body.PropertyMappings, "empty mapping lists must be sent explicitly")
			remote.Name = body.Name
			remote.PropertyMappings = slices.Clone(testProxyDefaultMappings)
			for _, id := range body.PropertyMappings {
				if !slices.Contains(remote.PropertyMappings, id) {
					remote.PropertyMappings = append(remote.PropertyMappings, id)
				}
			}
		}
		assert.NoError(t, json.NewEncoder(w).Encode(remote))
	}))
	t.Cleanup(server.Close)
	config := api.NewConfiguration()
	config.Servers = api.ServerConfigurations{{URL: server.URL + "/api/v3"}}
	config.HTTPClient = server.Client()
	return &APIClient{client: api.NewAPIClient(config)}, remote
}
