package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"pong": true}) })
	return r
}

func main() {
	_ = router()
}
