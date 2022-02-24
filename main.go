package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	e := echo.New()
	e.Use(
		middleware.RequestID(),
		middleware.Recover(),
		middleware.Logger(),
		middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20)),
		middleware.Timeout(),
		middleware.CORS(),
		middleware.Secure(),
	)

	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(os.Getenv("MONGODB_CONN_STRING")))
	if err != nil {
		e.Logger.Fatal(err)
	}

	db := client.Database("images-on-map")

	group := e.Group("/api/v1/markers")
	group.GET("/", func(c echo.Context) error {
		cursor, err := db.Collection("markers").Find(c.Request().Context(), bson.D{})
		if err != nil {
			c.Logger().Error(err)
			return c.JSON(http.StatusServiceUnavailable, Error{err})
		}

		results := []Marker{}
		if err := cursor.All(context.Background(), &results); err != nil {
			c.Logger().Error(err)
			return c.JSON(http.StatusServiceUnavailable, Error{err})
		}

		return c.JSON(http.StatusOK, results)
	})
	group.POST("/", func(c echo.Context) error {
		var body Marker
		if err := c.Bind(&body); err != nil {
			c.Logger().Info(err)
			return c.JSON(http.StatusBadRequest, Error{err})
		}

		if err := body.Validate(); err != nil {
			c.Logger().Info(err)
			return c.JSON(http.StatusBadRequest, Error{err})
		}

		if _, err := db.Collection("markers").InsertOne(c.Request().Context(), body.Normalize()); err != nil {
			var mongoErr mongo.WriteException
			if errors.As(err, &mongoErr) && mongoErr.HasErrorCode(11000) {
				s := "duplicated id"
				c.Logger().Info(s)
				return c.JSON(http.StatusBadRequest, ErrorString{s})
			}

			c.Logger().Error(err)
			return c.JSON(http.StatusServiceUnavailable, Error{err})
		}

		return c.NoContent(http.StatusCreated)
	})
	group.DELETE("/:id", func(c echo.Context) error {
		id := c.Param("id")
		if _, err := db.Collection("markers").DeleteOne(c.Request().Context(), bson.M{"_id": id}); err != nil {
			c.Logger().Error(err)
			return c.JSON(http.StatusServiceUnavailable, Error{err})
		}

		return c.NoContent(http.StatusOK)
	})
	group.PUT("/:id", func(c echo.Context) error {
		var body Marker
		if err := c.Bind(&body); err != nil {
			c.Logger().Info(err)
			return c.JSON(http.StatusBadRequest, Error{err})
		}

		id := c.Param("id")
		if body.ID != id {
			s := "id in path and body doesn't match"
			c.Logger().Info(s)
			return c.JSON(http.StatusBadRequest, ErrorString{s})
		}

		if err := body.Validate(); err != nil {
			c.Logger().Info(err)
			return c.JSON(http.StatusBadRequest, Error{err})
		}

		if _, err := db.Collection("markers").ReplaceOne(c.Request().Context(), bson.M{"_id": id}, body.Normalize()); err != nil {
			c.Logger().Error(err)
			return c.JSON(http.StatusServiceUnavailable, Error{err})
		}

		return c.NoContent(http.StatusOK)
	})

	e.Logger.Fatal(e.Start(":8080"))
}

type Error struct {
	Error error `json:"error"`
}

type ErrorString struct {
	Error string `json:"error"`
}

type Marker struct {
	ID       string  `json:"id" bson:"_id"`
	Name     string  `json:"name" bson:"name"`
	Location Coords  `json:"location" bson:"location"`
	Images   []Image `json:"images" bson:"images"`
}

func (m Marker) Normalize() Marker {
	if m.Images == nil {
		m.Images = []Image{}
	}

	return m
}

func (m Marker) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("empty id")
	}

	if m.Name == "" {
		return fmt.Errorf("empty name")
	}

	if err := m.Location.Validate(); err != nil {
		return fmt.Errorf("invalid location: %w", err)
	}

	for _, image := range m.Images {
		if err := image.Validate(); err != nil {
			return fmt.Errorf("invalid image %s: %w", image.ID, err)
		}
	}

	return nil
}

type Coords struct {
	Latitude  float64 `json:"latitude" bson:"latitude"`
	Longitude float64 `json:"longitude" bson:"longitude"`
}

func (c Coords) Validate() error {
	if c.Latitude < -180 || c.Latitude > 180 {
		return fmt.Errorf("invalid latitude")
	}

	if c.Longitude < -90 || c.Longitude > 90 {
		return fmt.Errorf("invalid longitude")
	}

	return nil
}

type Image struct {
	ID     string `json:"id" bson:"_id"`
	URI    string `json:"uri" bson:"uri"`
	Width  int    `json:"width" bson:"width"`
	Height int    `json:"height" bson:"height"`
}

func (i Image) Validate() error {
	if i.ID == "" {
		return fmt.Errorf("empty id")
	}

	if i.URI == "" {
		return fmt.Errorf("empty uri")
	}

	if i.Width <= 0 || i.Height <= 0 {
		return fmt.Errorf("invalid dimensions")
	}

	return nil
}
