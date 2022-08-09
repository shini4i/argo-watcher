// Package docs GENERATED BY SWAG; DO NOT EDIT
// This file was generated by swaggo/swag
package docs

import "github.com/swaggo/swag"

const docTemplate = `{
    "schemes": {{ marshal .Schemes }},
    "swagger": "2.0",
    "info": {
        "description": "{{escape .Description}}",
        "title": "{{.Title}}",
        "contact": {},
        "version": "{{.Version}}"
    },
    "host": "{{.Host}}",
    "basePath": "{{.BasePath}}",
    "paths": {
        "/api/v1/apps": {
            "get": {
                "description": "Get the list of apps",
                "tags": [
                    "frontend"
                ],
                "summary": "Get the list of apps",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "type": "array",
                            "items": {
                                "type": "string"
                            }
                        }
                    }
                }
            }
        },
        "/api/v1/tasks": {
            "get": {
                "description": "Get all tasks that match the provided parameters",
                "tags": [
                    "backend",
                    "frontend"
                ],
                "summary": "Get state content",
                "parameters": [
                    {
                        "type": "string",
                        "description": "App name",
                        "name": "app",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "default": 1648390029,
                        "description": "From timestamp",
                        "name": "from_timestamp",
                        "in": "query",
                        "required": true
                    },
                    {
                        "type": "integer",
                        "description": "To timestamp",
                        "name": "to_timestamp",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "type": "array",
                            "items": {
                                "$ref": "#/definitions/models.Task"
                            }
                        }
                    }
                }
            },
            "post": {
                "description": "Add a new task",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "backend"
                ],
                "summary": "Add a new task",
                "parameters": [
                    {
                        "description": "Task",
                        "name": "task",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/models.Task"
                        }
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/models.TaskStatus"
                        }
                    }
                }
            }
        },
        "/api/v1/tasks/{id}": {
            "get": {
                "description": "Get the status of a task",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "backend"
                ],
                "summary": "Get the status of a task",
                "parameters": [
                    {
                        "type": "string",
                        "default": "9185fae0-add5-11ec-87f3-56b185c552fa",
                        "description": "Task id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/models.TaskStatus"
                        }
                    }
                }
            }
        },
        "/api/v1/version": {
            "get": {
                "description": "Get the version of the server",
                "tags": [
                    "frontend"
                ],
                "summary": "Get the version of the server",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "type": "string"
                        }
                    }
                }
            }
        },
        "/healthz": {
            "get": {
                "description": "Check if the argo-watcher is ready to process new tasks",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "service"
                ],
                "summary": "Check if the server is healthy",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/models.HealthStatus"
                        }
                    },
                    "503": {
                        "description": "Service Unavailable",
                        "schema": {
                            "$ref": "#/definitions/models.HealthStatus"
                        }
                    }
                }
            }
        }
    },
    "definitions": {
        "models.HealthStatus": {
            "type": "object",
            "properties": {
                "status": {
                    "type": "string"
                }
            }
        },
        "models.Image": {
            "type": "object",
            "properties": {
                "image": {
                    "type": "string",
                    "example": "ghcr.io/shini4i/argo-watcher"
                },
                "tag": {
                    "type": "string",
                    "example": "dev"
                }
            }
        },
        "models.Task": {
            "type": "object",
            "required": [
                "app",
                "author",
                "images",
                "project"
            ],
            "properties": {
                "app": {
                    "type": "string",
                    "example": "argo-watcher"
                },
                "author": {
                    "type": "string",
                    "example": "John Doe"
                },
                "created": {
                    "type": "number"
                },
                "id": {
                    "type": "string"
                },
                "images": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/models.Image"
                    }
                },
                "project": {
                    "type": "string",
                    "example": "Demo"
                },
                "status": {
                    "type": "string"
                },
                "updated": {
                    "type": "number"
                }
            }
        },
        "models.TaskStatus": {
            "type": "object",
            "properties": {
                "id": {
                    "type": "string"
                },
                "status": {
                    "type": "string"
                }
            }
        }
    }
}`

// SwaggerInfo holds exported Swagger Info so clients can modify it
var SwaggerInfo = &swag.Spec{
	Version:          "",
	Host:             "",
	BasePath:         "",
	Schemes:          []string{},
	Title:            "",
	Description:      "",
	InfoInstanceName: "swagger",
	SwaggerTemplate:  docTemplate,
}

func init() {
	swag.Register(SwaggerInfo.InstanceName(), SwaggerInfo)
}