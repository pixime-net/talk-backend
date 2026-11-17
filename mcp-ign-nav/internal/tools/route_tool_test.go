package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

func TestRouteTool_Metadata(t *testing.T) {
	tool := NewRouteTool(rate.NewLimiter(rate.Inf, 0))
	if tool.Name() != "route" {
		t.Errorf("unexpected tool name: %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestRouteTool_Call_RoadNameFormats(t *testing.T) {
	tests := []struct {
		name       string
		roadName   string
		wantName   string
		wantNumber string
		wantPlace  string
		wantError  bool
	}{
		{name: "string", roadName: `"Autoroute du Soleil"`, wantName: "Autoroute du Soleil"},
		{name: "empty string", roadName: `""`},
		{name: "object left", roadName: `{"nom_1_gauche":"Rue gauche","nom_1_droite":"Rue droite","cpx_numero":"A6","cpx_toponyme":"Bourgogne"}`, wantName: "Rue gauche", wantNumber: "A6", wantPlace: "Bourgogne"},
		{name: "object right", roadName: `{"nom_1_droite":"Rue droite"}`, wantName: "Rue droite"},
		{name: "null", roadName: `null`},
		{name: "invalid number", roadName: `42`, wantError: true},
		{name: "invalid array", roadName: `[]`, wantError: true},
		{name: "invalid object field", roadName: `{"nom_1_gauche":42}`, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, err := fmt.Fprintf(w, `{"geometry":{"type":"LineString","coordinates":[[4.835,45.758],[2.347,48.859]]},"portions":[{"steps":[{"attributes":{"name":%s}}]}]}`, tt.roadName)
				if err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer srv.Close()

			tool := newRouteToolWithBaseURL(srv.URL, srv.Client())
			result, err := tool.Call(context.Background(), RouteToolInput{Start: "4.835,45.758", End: "2.347,48.859"})
			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), "decoding response") {
					t.Fatalf("expected decoding error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Portions) != 1 || len(result.Portions[0].Steps) != 1 {
				t.Fatalf("unexpected portions: %+v", result.Portions)
			}
			step := result.Portions[0].Steps[0]
			if step.Name != tt.wantName || step.RoadNumber != tt.wantNumber || step.Toponyme != tt.wantPlace {
				t.Errorf("got name %q, road number %q, toponyme %q; want %q, %q, %q", step.Name, step.RoadNumber, step.Toponyme, tt.wantName, tt.wantNumber, tt.wantPlace)
			}
		})
	}
}

func TestRouteTool_Call_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %q", r.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(r.Body)
		var req routeRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if req.Start != "2.337306,48.849319" {
			t.Errorf("unexpected start: %q", req.Start)
		}
		if req.End != "2.367776,48.852891" {
			t.Errorf("unexpected end: %q", req.End)
		}
		if req.Resource != "bdtopo-osrm" {
			t.Errorf("unexpected resource: %q", req.Resource)
		}
		if req.Profile != "car" {
			t.Errorf("unexpected profile: %q", req.Profile)
		}
		if req.Optimization != "fastest" {
			t.Errorf("unexpected optimization: %q", req.Optimization)
		}
		if req.GetSteps != "true" {
			t.Errorf("expected getSteps 'true', got %q", req.GetSteps)
		}

		resp := routeAPIResponse{
			Start:        "2.337325,48.84932",
			End:          "2.367842,48.85278",
			Profile:      "car",
			Optimization: "fastest",
			Distance:     2562.9,
			Duration:     581.1,
			Bbox:         []float64{2.337325, 48.848823, 2.367842, 48.85278},
			Geometry: &GeoJSONGeometry{
				Type:        "LineString",
				Coordinates: [][]float64{{2.337325, 48.84932}, {2.367842, 48.85278}},
			},
			Portions: []routeAPIPortion{
				{
					Start:    "2.337325,48.84932",
					End:      "2.367842,48.85278",
					Distance: 2562.9,
					Duration: 581.1,
					Steps: []routeAPIStep{
						{
							Distance:    2.5,
							Duration:    0.3,
							Instruction: routeAPIInstruction{Type: "depart"},
							Attributes:  routeAPIAttributes{Name: routeAPIName{NomGauche: "R DE TOURNON"}},
							Geometry: &GeoJSONGeometry{
								Type:        "LineString",
								Coordinates: [][]float64{{2.337325, 48.84932}, {2.337400, 48.849400}},
							},
						},
						{
							Distance:    106.8,
							Duration:    30.4,
							Instruction: routeAPIInstruction{Type: "turn", Modifier: "left"},
							Attributes:  routeAPIAttributes{Name: routeAPIName{NomGauche: "R DE VAUGIRARD"}},
							Geometry: &GeoJSONGeometry{
								Type:        "LineString",
								Coordinates: [][]float64{{2.337400, 48.849400}, {2.338500, 48.850100}, {2.339200, 48.850500}},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tool := newRouteToolWithBaseURL(srv.URL, srv.Client())
	result, err := tool.Call(context.Background(), RouteToolInput{
		Start: "2.337306,48.849319",
		End:   "2.367776,48.852891",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Distance != 2562.9 {
		t.Errorf("unexpected distance: %f", result.Distance)
	}
	if result.Duration != 581.1 {
		t.Errorf("unexpected duration: %f", result.Duration)
	}
	if result.Profile != "car" {
		t.Errorf("unexpected profile: %q", result.Profile)
	}
	if result.Optimization != "fastest" {
		t.Errorf("unexpected optimization: %q", result.Optimization)
	}
	if len(result.Portions) != 1 {
		t.Fatalf("expected 1 portion, got %d", len(result.Portions))
	}
	if result.Portions[0].Distance != 2562.9 {
		t.Errorf("unexpected portion distance: %f", result.Portions[0].Distance)
	}
	if len(result.Portions[0].Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(result.Portions[0].Steps))
	}
	if result.Portions[0].Steps[0].Instruction != "depart" {
		t.Errorf("unexpected step instruction: %q", result.Portions[0].Steps[0].Instruction)
	}
	if result.Portions[0].Steps[0].Name != "R DE TOURNON" {
		t.Errorf("unexpected step name: %q", result.Portions[0].Steps[0].Name)
	}
	if result.Portions[0].Steps[0].Start != "2.337325,48.84932" {
		t.Errorf("unexpected step start: %q", result.Portions[0].Steps[0].Start)
	}
	if result.Portions[0].Steps[0].End != "2.3374,48.8494" {
		t.Errorf("unexpected step end: %q", result.Portions[0].Steps[0].End)
	}
	if result.Portions[0].Steps[1].Modifier != "left" {
		t.Errorf("unexpected step modifier: %q", result.Portions[0].Steps[1].Modifier)
	}
	if result.Portions[0].Steps[1].Start != "2.3374,48.8494" {
		t.Errorf("unexpected step 1 start: %q", result.Portions[0].Steps[1].Start)
	}
	if result.Portions[0].Steps[1].End != "2.3392,48.8505" {
		t.Errorf("unexpected step 1 end: %q", result.Portions[0].Steps[1].End)
	}
	if result.Geometry == nil {
		t.Fatal("expected geometry to be non-nil")
	}
	if result.Geometry.Type != "LineString" {
		t.Errorf("unexpected geometry type: %q", result.Geometry.Type)
	}
}

func TestRouteTool_Call_WithIntermediates(t *testing.T) {
	var receivedReq routeRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedReq)

		resp := routeAPIResponse{
			Start:        "2.337325,48.84932",
			End:          "2.367842,48.85278",
			Profile:      "car",
			Optimization: "fastest",
			Distance:     2563.0,
			Duration:     581.1,
			Bbox:         []float64{2.337325, 48.848823, 2.367842, 48.85278},
			Geometry: &GeoJSONGeometry{
				Type:        "LineString",
				Coordinates: [][]float64{{2.337325, 48.84932}, {2.367842, 48.85278}},
			},
			Portions: []routeAPIPortion{
				{Start: "2.337325,48.84932", End: "2.349861,48.84976", Distance: 1159.4, Duration: 280.7},
				{Start: "2.349861,48.84976", End: "2.367842,48.85278", Distance: 1403.6, Duration: 300.4},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tool := newRouteToolWithBaseURL(srv.URL, srv.Client())
	result, err := tool.Call(context.Background(), RouteToolInput{
		Start:         "2.337306,48.849319",
		End:           "2.367776,48.852891",
		Intermediates: []string{"2.350000,48.850000"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(receivedReq.Intermediates) != 1 || receivedReq.Intermediates[0] != "2.350000,48.850000" {
		t.Errorf("unexpected intermediates in request: %v", receivedReq.Intermediates)
	}
	if len(result.Portions) != 2 {
		t.Fatalf("expected 2 portions, got %d", len(result.Portions))
	}
}

func TestRouteTool_Call_MissingStart(t *testing.T) {
	tool := NewRouteTool(rate.NewLimiter(rate.Inf, 0))
	_, err := tool.Call(context.Background(), RouteToolInput{End: "2.367776,48.852891"})
	if err == nil {
		t.Error("expected error for missing start")
	}
}

func TestRouteTool_Call_MissingEnd(t *testing.T) {
	tool := NewRouteTool(rate.NewLimiter(rate.Inf, 0))
	_, err := tool.Call(context.Background(), RouteToolInput{Start: "2.337306,48.849319"})
	if err == nil {
		t.Error("expected error for missing end")
	}
}

func TestRouteTool_Call_GeometryFromSteps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := routeAPIResponse{
			Start: "2.33,48.84", End: "2.37,48.85",
			Profile: "car", Optimization: "fastest",
			Distance: 4000, Duration: 600, Bbox: []float64{2.33, 48.84, 2.37, 48.85},
			// Geometry intentionally absent (nil) — simulates bdtopo-osrm behavior.
			Portions: []routeAPIPortion{{
				Start: "2.33,48.84", End: "2.37,48.85", Distance: 4000, Duration: 600,
				Steps: []routeAPIStep{
					{
						Distance: 2000, Duration: 300,
						Instruction: routeAPIInstruction{Type: "depart"},
						Geometry: &GeoJSONGeometry{
							Type:        "LineString",
							Coordinates: [][]float64{{2.33, 48.84}, {2.35, 48.845}},
						},
					},
					{
						Distance: 2000, Duration: 300,
						Instruction: routeAPIInstruction{Type: "arrive"},
						Geometry: &GeoJSONGeometry{
							Type:        "LineString",
							Coordinates: [][]float64{{2.35, 48.845}, {2.37, 48.85}},
						},
					},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tool := newRouteToolWithBaseURL(srv.URL, srv.Client())
	result, err := tool.Call(context.Background(), RouteToolInput{
		Start: "2.33,48.84",
		End:   "2.37,48.85",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Geometry == nil {
		t.Fatal("expected geometry to be built from step geometries")
	}
	if result.Geometry.Type != "LineString" {
		t.Errorf("geometry type = %q, want LineString", result.Geometry.Type)
	}
	// 3 unique points: [2.33,48.84], [2.35,48.845], [2.37,48.85] — first of step 2 is deduplicated.
	if len(result.Geometry.Coordinates) != 3 {
		t.Errorf("got %d coordinates, want 3", len(result.Geometry.Coordinates))
	}
}

func TestRouteTool_Call_NilGeometry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := routeAPIResponse{
			Start: "2.337325,48.84932", End: "2.367842,48.85278",
			Profile: "car", Optimization: "fastest",
			Distance: 100, Duration: 50, Bbox: []float64{0, 0, 1, 1},
			Portions: []routeAPIPortion{{Start: "a", End: "b", Distance: 100, Duration: 50}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tool := newRouteToolWithBaseURL(srv.URL, srv.Client())
	_, err := tool.Call(context.Background(), RouteToolInput{
		Start: "2.337306,48.849319",
		End:   "2.367776,48.852891",
	})
	if err == nil {
		t.Error("expected error when API returns no geometry")
	}
}

func TestRouteTool_Call_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		resp := routeErrorResponse{}
		resp.Error.Message = "Parameter 'profile' is invalid"
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tool := newRouteToolWithBaseURL(srv.URL, srv.Client())
	_, err := tool.Call(context.Background(), RouteToolInput{
		Start: "2.337306,48.849319",
		End:   "2.367776,48.852891",
	})
	if err == nil {
		t.Error("expected error for API error response")
	}
}

func TestRouteTool_Call_Defaults(t *testing.T) {
	var receivedReq routeRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedReq)

		resp := routeAPIResponse{
			Start: "2.337325,48.84932", End: "2.367842,48.85278",
			Profile: "car", Optimization: "fastest",
			Distance: 100, Duration: 50, Bbox: []float64{0, 0, 1, 1},
			Geometry: &GeoJSONGeometry{Type: "LineString", Coordinates: [][]float64{{0, 0}, {1, 1}}},
			Portions: []routeAPIPortion{{Start: "a", End: "b", Distance: 100, Duration: 50}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tool := newRouteToolWithBaseURL(srv.URL, srv.Client())
	_, _ = tool.Call(context.Background(), RouteToolInput{
		Start: "2.337306,48.849319",
		End:   "2.367776,48.852891",
	})

	if receivedReq.Resource != "bdtopo-osrm" {
		t.Errorf("expected default resource 'bdtopo-osrm', got %q", receivedReq.Resource)
	}
	if receivedReq.Profile != "car" {
		t.Errorf("expected default profile 'car', got %q", receivedReq.Profile)
	}
	if receivedReq.Optimization != "fastest" {
		t.Errorf("expected default optimization 'fastest', got %q", receivedReq.Optimization)
	}
}

func TestRouteTool_Call_AvoidHighways(t *testing.T) {
	var receivedReq routeRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedReq)

		resp := routeAPIResponse{
			Start: "2.337325,48.84932", End: "2.367842,48.85278",
			Profile: "car", Optimization: "fastest",
			Distance: 3000, Duration: 900, Bbox: []float64{0, 0, 1, 1},
			Geometry: &GeoJSONGeometry{Type: "LineString", Coordinates: [][]float64{{0, 0}, {1, 1}}},
			Portions: []routeAPIPortion{{Start: "a", End: "b", Distance: 3000, Duration: 900}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tool := newRouteToolWithBaseURL(srv.URL, srv.Client())
	_, err := tool.Call(context.Background(), RouteToolInput{
		Start:         "2.337306,48.849319",
		End:           "2.367776,48.852891",
		AvoidHighways: "true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(receivedReq.Constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(receivedReq.Constraints))
	}
	c := receivedReq.Constraints[0]
	if c.ConstraintType != "banned" {
		t.Errorf("expected constraintType 'banned', got %q", c.ConstraintType)
	}
	if c.Key != "wayType" {
		t.Errorf("expected key 'wayType', got %q", c.Key)
	}
	if c.Operator != "=" {
		t.Errorf("expected operator '=', got %q", c.Operator)
	}
	if c.Value != "autoroute" {
		t.Errorf("expected value 'autoroute', got %q", c.Value)
	}
}

func TestRouteTool_Call_NoAvoidHighways(t *testing.T) {
	var receivedReq routeRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedReq)

		resp := routeAPIResponse{
			Start: "2.337325,48.84932", End: "2.367842,48.85278",
			Profile: "car", Optimization: "fastest",
			Distance: 100, Duration: 50, Bbox: []float64{0, 0, 1, 1},
			Geometry: &GeoJSONGeometry{Type: "LineString", Coordinates: [][]float64{{0, 0}, {1, 1}}},
			Portions: []routeAPIPortion{{Start: "a", End: "b", Distance: 100, Duration: 50}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tool := newRouteToolWithBaseURL(srv.URL, srv.Client())
	_, err := tool.Call(context.Background(), RouteToolInput{
		Start: "2.337306,48.849319",
		End:   "2.367776,48.852891",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(receivedReq.Constraints) != 0 {
		t.Errorf("expected no constraints, got %d", len(receivedReq.Constraints))
	}
}
