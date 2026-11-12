package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"lingoway/internal/catalog"
	"lingoway/internal/middleware"
	"lingoway/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	catalogDefaultLimit    = 20
	catalogMaxLimit        = 100
	catalogMaxFilterValues = 20
	catalogMaxSearchLength = 100
)

var catalogLevels = map[string]bool{"A1": true, "A2": true, "B1": true, "B2": true, "C1": true, "C2": true}

type catalogHTTPHandler struct {
	service *catalog.Service
}

// registerCatalogRoutes подключает темы и каталог материалов. Группа protected уже требует вход (middleware.Auth), поэтому claims в обработчиках всегда есть.
func registerCatalogRoutes(protected *gin.RouterGroup, database *gorm.DB) {
	handler := catalogHTTPHandler{service: catalog.NewService(database)}

	protected.GET("/topics", privateNoStore(), handler.listTopics)

	group := protected.Group("/catalog", privateNoStore())
	{
		group.GET("/materials", handler.listMaterials)
		group.GET("/materials/:id", handler.getMaterial)
		group.PUT("/materials/:id/favorite", handler.addFavorite)
		group.DELETE("/materials/:id/favorite", handler.removeFavorite)
	}
}

// privateNoStore запрещает кэшировать ответы: избранное зависит от пользователя.
func privateNoStore() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Next()
	}
}

// --- публичные структуры ответов (DTO) ---

type catalogPage[T any] struct {
	Items   []T  `json:"items"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

type topicResponse struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type catalogItemResponse struct {
	ID          int64                      `json:"id"`
	TopicID     int64                      `json:"topic_id"`
	Level       models.CEFRLevel           `json:"level"`
	Type        models.CatalogMaterialType `json:"type"`
	Title       string                     `json:"title"`
	Description *string                    `json:"description"`
	URL         *string                    `json:"url"`
	IsFavorite  bool                       `json:"is_favorite"`
	CreatedAt   time.Time                  `json:"created_at"`
}

type catalogDetailResponse struct {
	catalogItemResponse
	Body      *string   `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
}

// --- обработчики ---

func (h catalogHTTPHandler) listTopics(c *gin.Context) {
	limit, offset, ok := parsePagination(c)
	if !ok {
		return
	}
	items, hasMore, err := h.service.ListTopics(c.Request.Context(), limit, offset)
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	response := make([]topicResponse, 0, len(items))
	for _, topic := range items {
		response = append(response, topicResponse{ID: topic.ID, Slug: topic.Slug, Name: topic.Name})
	}
	c.JSON(http.StatusOK, catalogPage[topicResponse]{Items: response, Limit: limit, Offset: offset, HasMore: hasMore})
}

func (h catalogHTTPHandler) listMaterials(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	filter, ok := parseCatalogFilter(c)
	if !ok {
		return
	}
	items, hasMore, err := h.service.ListMaterials(c.Request.Context(), userID, filter)
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	response := make([]catalogItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, catalogItemResponse{
			ID: item.ID, TopicID: item.TopicID, Level: item.Level, Type: item.Type,
			Title: item.Title, Description: item.Description, URL: item.URL,
			IsFavorite: item.IsFavorite, CreatedAt: item.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, catalogPage[catalogItemResponse]{
		Items: response, Limit: filter.Limit, Offset: filter.Offset, HasMore: hasMore,
	})
}

func (h catalogHTTPHandler) getMaterial(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	materialID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	detail, err := h.service.GetMaterial(c.Request.Context(), userID, materialID)
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, catalogDetailResponse{
		catalogItemResponse: catalogItemResponse{
			ID: detail.ID, TopicID: detail.TopicID, Level: detail.Level, Type: detail.Type,
			Title: detail.Title, Description: detail.Description, URL: detail.URL,
			IsFavorite: detail.IsFavorite, CreatedAt: detail.CreatedAt,
		},
		Body:      detail.Body,
		UpdatedAt: detail.UpdatedAt,
	})
}

func (h catalogHTTPHandler) addFavorite(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	materialID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	if err := h.service.AddFavorite(c.Request.Context(), userID, materialID); err != nil {
		writeCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h catalogHTTPHandler) removeFavorite(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	materialID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	if err := h.service.RemoveFavorite(c.Request.Context(), userID, materialID); err != nil {
		writeCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// --- разбор запроса и ошибки ---

// currentUserID берёт пользователя из токена, а не из тела запроса.
func currentUserID(c *gin.Context) (int64, bool) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		writeAPIError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Требуется вход в систему.")
		return 0, false
	}
	return claims.UserID, true
}

func parseCatalogID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		writeAPIError(c, http.StatusBadRequest, "INVALID_ID", "Некорректный идентификатор.")
		return 0, false
	}
	return id, true
}

func parsePagination(c *gin.Context) (limit, offset int, ok bool) {
	limit, offset = catalogDefaultLimit, 0
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > catalogMaxLimit {
			writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "Параметр limit должен быть числом от 1 до 100.")
			return 0, 0, false
		}
		limit = value
	}
	if raw := c.Query("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "Параметр offset должен быть неотрицательным числом.")
			return 0, 0, false
		}
		offset = value
	}
	return limit, offset, true
}

// splitQueryValues принимает и «topic_id=1,3», и «topic_id=1&topic_id=3».
func splitQueryValues(values []string) []string {
	var parts []string
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
	}
	return parts
}

func parseCatalogFilter(c *gin.Context) (catalog.ListFilter, bool) {
	var filter catalog.ListFilter
	var ok bool
	if filter.Limit, filter.Offset, ok = parsePagination(c); !ok {
		return filter, false
	}

	topicParts := splitQueryValues(c.QueryArray("topic_id"))
	if len(topicParts) > catalogMaxFilterValues {
		writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "Слишком много значений в topic_id.")
		return filter, false
	}
	for _, part := range topicParts {
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id < 1 {
			writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "topic_id должен быть списком положительных чисел.")
			return filter, false
		}
		filter.TopicIDs = append(filter.TopicIDs, id)
	}

	levelParts := splitQueryValues(c.QueryArray("level"))
	if len(levelParts) > catalogMaxFilterValues {
		writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "Слишком много значений в level.")
		return filter, false
	}
	for _, part := range levelParts {
		level := strings.ToUpper(part)
		if !catalogLevels[level] {
			writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "level допускает значения A1, A2, B1, B2, C1, C2.")
			return filter, false
		}
		filter.Levels = append(filter.Levels, level)
	}

	switch c.Query("favorite") {
	case "":
	case "true":
		value := true
		filter.Favorite = &value
	case "false":
		value := false
		filter.Favorite = &value
	default:
		writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "favorite допускает значения true и false.")
		return filter, false
	}

	filter.Search = strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(filter.Search) > catalogMaxSearchLength {
		writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "Поисковый запрос не длиннее 100 символов.")
		return filter, false
	}
	return filter, true
}

// writeCatalogError не отдаёт клиенту текст внутренних ошибок, только пишет их в лог.
func writeCatalogError(c *gin.Context, err error) {
	if errors.Is(err, catalog.ErrNotFound) {
		writeAPIError(c, http.StatusNotFound, "NOT_FOUND", "Материал не найден.")
		return
	}
	log.Printf("catalog: %v", err)
	writeAPIError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Не удалось выполнить запрос.")
}
