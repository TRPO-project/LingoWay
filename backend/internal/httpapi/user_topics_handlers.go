package httpapi

import (
	"errors"
	"net/http"

	"lingoway/internal/catalog"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const userTopicsMaxBodyBytes = 1 << 14

type userTopicHTTPHandler struct {
	service *catalog.Service
}

// registerUserTopicRoutes подключает выбор тем текущего пользователя. Группа protected
// уже требует вход и проверяет пользователя в БД (Auth и ActiveUser).
func registerUserTopicRoutes(protected *gin.RouterGroup, database *gorm.DB) {
	handler := userTopicHTTPHandler{service: catalog.NewService(database)}

	group := protected.Group("/me/topics", privateNoStore())
	{
		group.GET("", handler.get)
		group.PUT("", handler.put)
	}
}

type userTopicResponse struct {
	ID       int64  `json:"id"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// userTopicsResponse содержит max_topics, чтобы фронтенд не хардкодил лимит.
type userTopicsResponse struct {
	Items     []userTopicResponse `json:"items"`
	MaxTopics int                 `json:"max_topics"`
}

type userTopicsRequest struct {
	TopicIDs []int64 `json:"topic_ids"`
}

func newUserTopicsResponse(items []catalog.SelectedTopic) userTopicsResponse {
	response := userTopicsResponse{Items: make([]userTopicResponse, 0, len(items)), MaxTopics: catalog.MaxUserTopics}
	for _, item := range items {
		response.Items = append(response.Items, userTopicResponse{
			ID: item.TopicID, Slug: item.Slug, Name: item.Name, Position: item.Position,
		})
	}
	return response
}

func (h userTopicHTTPHandler) get(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	items, err := h.service.GetSelectedTopics(c.Request.Context(), userID)
	if err != nil {
		writeUserTopicsError(c, err)
		return
	}
	c.JSON(http.StatusOK, newUserTopicsResponse(items))
}

func (h userTopicHTTPHandler) put(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var request userTopicsRequest
	if !decodeStrictJSON(c, &request, userTopicsMaxBodyBytes) {
		return
	}
	items, err := h.service.SetSelectedTopics(c.Request.Context(), userID, request.TopicIDs)
	if err != nil {
		writeUserTopicsError(c, err)
		return
	}
	c.JSON(http.StatusOK, newUserTopicsResponse(items))
}

func writeUserTopicsError(c *gin.Context, err error) {
	if errors.Is(err, catalog.ErrUserNotFound) {
		writeAPIError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Пользователь не найден. Войдите заново.")
		return
	}
	// Ошибки проверки данных, TOPIC_NOT_FOUND и внутренние ошибки обрабатывает общий обработчик.
	writeAdminCatalogError(c, err)
}
