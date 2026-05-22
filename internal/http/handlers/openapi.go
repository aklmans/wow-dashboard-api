package handlers

import (
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

const apiErrorResponseRef = "#/components/responses/APIError"

func apiErrorResponses(api huma.API, statusCodes ...int) map[string]*huma.Response {
	registerAPIErrorResponse(api)

	responses := make(map[string]*huma.Response, len(statusCodes))
	for _, statusCode := range statusCodes {
		responses[strconv.Itoa(statusCode)] = &huma.Response{Ref: apiErrorResponseRef}
	}
	return responses
}

func registerAPIErrorResponse(api huma.API) {
	spec := api.OpenAPI()
	if spec.Components == nil {
		spec.Components = &huma.Components{}
	}
	if spec.Components.Responses == nil {
		spec.Components.Responses = map[string]*huma.Response{}
	}
	if _, ok := spec.Components.Responses["APIError"]; ok {
		return
	}

	spec.Components.Responses["APIError"] = &huma.Response{
		Description: "Project API error envelope",
		Content: map[string]*huma.MediaType{
			"application/json": {
				Schema: apiErrorResponseSchema(),
			},
		},
	}
}

func apiErrorResponseSchema() *huma.Schema {
	return &huma.Schema{
		Type:                 "object",
		AdditionalProperties: false,
		Required:             []string{"code", "message", "request_id"},
		Properties: map[string]*huma.Schema{
			"code": {
				Type:        "string",
				Description: "Machine-readable error code",
				Examples:    []any{"unauthorized"},
			},
			"message": {
				Type:        "string",
				Description: "Safe client-facing error message",
				Examples:    []any{"Authorization token missing or invalid."},
			},
			"request_id": {
				Type:        "string",
				Description: "Request ID for tracing",
				Examples:    []any{"abc-123-def"},
			},
			"details": {
				Type:        "array",
				Description: "Optional field-level error details",
				Items: &huma.Schema{
					Type:                 "object",
					AdditionalProperties: false,
					Required:             []string{"message"},
					Properties: map[string]*huma.Schema{
						"field": {
							Type:        "string",
							Description: "Field associated with the error",
						},
						"message": {
							Type:        "string",
							Description: "Safe detail message",
						},
					},
				},
			},
		},
	}
}
