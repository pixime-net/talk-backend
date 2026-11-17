package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/time/rate"
)

// routeParams holds the common input parameters for a route API call.
type routeParams struct {
	Start         string
	End           string
	Resource      string
	Profile       string
	Optimization  string
	Intermediates []string
	AvoidHighways string
	GetSteps      bool
	GetGeometry   bool
}

// routeClient adapts the shared HTTP transport for route API calls.
type routeClient struct {
	*httpClient
}

// newRouteClient creates a routeClient with the given base URL and limiter.
func newRouteClient(baseURL string, httpClient *http.Client, limiter *rate.Limiter) *routeClient {
	return &routeClient{httpClient: newHTTPClient(baseURL, httpClient, limiter)}
}

// callRouteAPI calls the IGN /itineraire endpoint with the given parameters.
func (c *routeClient) callRouteAPI(ctx context.Context, params routeParams) (*routeAPIResponse, error) {
	if params.Start == "" {
		return nil, fmt.Errorf("parameter 'start' is required")
	}
	if params.End == "" {
		return nil, fmt.Errorf("parameter 'end' is required")
	}

	body := buildRouteRequest(params)

	var result routeAPIResponse
	if err := c.postJSON(ctx, "/itineraire", body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// buildRouteRequest applies defaults and assembles the JSON body for the IGN /itineraire endpoint.
func buildRouteRequest(params routeParams) routeRequest {
	resource := params.Resource
	if resource == "" {
		resource = "bdtopo-osrm"
	}
	profile := params.Profile
	if profile == "" {
		profile = "car"
	}
	optimization := params.Optimization
	if optimization == "" {
		optimization = "fastest"
	}
	body := routeRequest{
		Start:        params.Start,
		End:          params.End,
		Resource:     resource,
		Profile:      profile,
		Optimization: optimization,
	}
	if params.GetSteps {
		body.GetSteps = "true"
		body.GetBbox = "true"
		body.GeometryFormat = "geojson"
	}
	if params.GetGeometry {
		body.GetGeometry = "true"
		body.GeometryFormat = "geojson"
	}
	if len(params.Intermediates) > 0 {
		body.Intermediates = params.Intermediates
	}
	if params.AvoidHighways == "true" {
		body.Constraints = []routeConstraint{{
			ConstraintType: "banned",
			Key:            "wayType",
			Operator:       "=",
			Value:          "autoroute",
		}}
	}
	return body
}

// routeConstraint represents a routing constraint for the IGN API.
type routeConstraint struct {
	ConstraintType string `json:"constraintType"`
	Key            string `json:"key"`
	Operator       string `json:"operator"`
	Value          string `json:"value"`
}

// routeRequest is the JSON body sent to the IGN /itineraire endpoint.
type routeRequest struct {
	Start          string            `json:"start"`
	End            string            `json:"end"`
	Resource       string            `json:"resource"`
	Profile        string            `json:"profile"`
	Optimization   string            `json:"optimization"`
	GetSteps       string            `json:"getSteps,omitempty"`
	GetGeometry    string            `json:"getGeometry,omitempty"`
	GeometryFormat string            `json:"geometryFormat,omitempty"`
	GetBbox        string            `json:"getBbox,omitempty"`
	Intermediates  []string          `json:"intermediates,omitempty"`
	Constraints    []routeConstraint `json:"constraints,omitempty"`
}

// routeAPIResponse is the raw JSON response from the IGN /itineraire endpoint.
type routeAPIResponse struct {
	Start        string            `json:"start"`
	End          string            `json:"end"`
	Profile      string            `json:"profile"`
	Optimization string            `json:"optimization"`
	Distance     float64           `json:"distance"`
	Duration     float64           `json:"duration"`
	Bbox         []float64         `json:"bbox"`
	Geometry     *GeoJSONGeometry  `json:"geometry"`
	Portions     []routeAPIPortion `json:"portions"`
}

type routeAPIPortion struct {
	Start    string         `json:"start"`
	End      string         `json:"end"`
	Distance float64        `json:"distance"`
	Duration float64        `json:"duration"`
	Steps    []routeAPIStep `json:"steps"`
}

type routeAPIStep struct {
	Distance    float64             `json:"distance"`
	Duration    float64             `json:"duration"`
	Instruction routeAPIInstruction `json:"instruction"`
	Attributes  routeAPIAttributes  `json:"attributes"`
	Geometry    *GeoJSONGeometry    `json:"geometry"`
}

type routeAPIInstruction struct {
	Type     string `json:"type"`
	Modifier string `json:"modifier"`
}

type routeAPIAttributes struct {
	Name routeAPIName `json:"name"`
}

type routeAPIName struct {
	NomGauche   string `json:"nom_1_gauche"`
	NomDroite   string `json:"nom_1_droite"`
	CpxNumero   string `json:"cpx_numero"`
	CpxToponyme string `json:"cpx_toponyme"`
}

func (n *routeAPIName) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return err
		}
		*n = routeAPIName{NomGauche: name}
		return nil
	}

	type routeAPINameObject routeAPIName
	var name routeAPINameObject
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	*n = routeAPIName(name)
	return nil
}

// routeErrorResponse is the error format from the IGN navigation API.
type routeErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}
