package handler

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Swagger khusus integrasi tracker FMC150 / FMC650: hanya endpoint HTTP yang berhubungan
// dengan tracker (status port TCP, devices, records, perintah Codec 12, log). Koneksi tracker
// sendiri lewat TCP Codec 8E di DEVICE_PORT, dijelaskan di deskripsi spesifikasinya.

//go:embed docs/fmc-openapi.yaml
var fmcOpenAPI []byte

const fmcSwaggerHTML = `<!doctype html>
<html lang="id">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>FMS – Swagger FMC</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: "/swagger-fmc/openapi.yaml",
      dom_id: "#swagger-ui",
      persistAuthorization: true,
      docExpansion: "list",
      tryItOutEnabled: true
    });
  </script>
</body>
</html>`

func registerFmcSwagger(r *gin.Engine) {
	r.GET("/swagger-fmc", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmcSwaggerHTML))
	})
	r.GET("/swagger-fmc/openapi.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", fmcOpenAPI)
	})
}
