package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/lib/validation"
	"github.com/Karan0009/wordotron_api/app/models"
	"github.com/Karan0009/wordotron_api/app/serializers"
	"github.com/Karan0009/wordotron_api/app/services"
)

// wordSortWhitelist restricts ?sort= to indexed columns, plus "random" for
// shuffled browsing (not a stable order across pages - see ListWords in
// sql/queries/words.sql).
var wordSortWhitelist = lib.SortWhitelist{
	Allowed: []string{"created_at", "updated_at", "term", "random"},
	Default: "created_at",
}

// WordHandler exposes the vocabulary catalogue endpoints.
type WordHandler struct {
	base
	words services.WordService
}

// NewWordHandler builds the word handler.
func NewWordHandler(words services.WordService, validator *validation.Validator) *WordHandler {
	return &WordHandler{
		base:  base{validator: validator},
		words: words,
	}
}

// Create adds a term to the catalogue and enriches it (definitions, parts of
// speech, synonyms, antonyms, alternate phrasings) via the configured
// Enricher.
//
//	@Summary		Add a word
//	@Description	Creates a term and fills in its senses via AI enrichment. Fails if the term already exists.
//	@Tags			words
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.CreateWordRequest	true	"Term to add"
//	@Success		201		{object}	lib.SuccessEnvelope{data=serializers.WordResponse}
//	@Failure		409		{object}	lib.ErrorEnvelope
//	@Failure		503		{object}	lib.ErrorEnvelope
//	@Router			/words [post]
func (h *WordHandler) Create(c fiber.Ctx) error {
	actor, err := actorFrom(c)
	if err != nil {
		return err
	}

	var req serializers.CreateWordRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	word, err := h.words.CreateAndEnrich(c.Context(), actor, services.CreateWordInput{
		Term:     req.Term,
		Language: req.Language,
	})
	if err != nil {
		return err
	}

	return lib.Created(c, serializers.ToWordResponse(word))
}

// Get returns a single word with its senses.
//
//	@Summary		Get a word
//	@Tags			words
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Word ID"	format(uuid)
//	@Success		200	{object}	lib.SuccessEnvelope{data=serializers.WordResponse}
//	@Failure		404	{object}	lib.ErrorEnvelope
//	@Router			/words/{id} [get]
func (h *WordHandler) Get(c fiber.Ctx) error {
	id, err := uuidParam(c, "id")
	if err != nil {
		return err
	}

	word, err := h.words.Get(c.Context(), id)
	if err != nil {
		return err
	}
	return lib.OK(c, serializers.ToWordResponse(word))
}

// List returns a paginated, filtered page of words (senses not expanded).
//
//	@Summary		List words
//	@Tags			words
//	@Security		BearerAuth
//	@Produce		json
//	@Param			page		query		int		false	"Page number"	default(1)
//	@Param			limit		query		int		false	"Page size (max 100)"	default(20)
//	@Param			sort		query		string	false	"Sort column"	Enums(created_at, updated_at, term, random)
//	@Param			order		query		string	false	"Sort direction"	Enums(asc, desc)
//	@Param			search		query		string	false	"Match against term"
//	@Param			language	query		string	false	"Filter by language"
//	@Success		200			{object}	lib.SuccessEnvelope{data=[]serializers.WordResponse,meta=models.PageMeta}
//	@Router			/words [get]
func (h *WordHandler) List(c fiber.Ctx) error {
	page, err := lib.ParsePageParams(c, wordSortWhitelist)
	if err != nil {
		return err
	}

	filter := models.ListWordsFilter{}
	if raw := c.Query("search"); raw != "" {
		filter.Search = &raw
	}
	if raw := c.Query("language"); raw != "" {
		filter.Language = &raw
	}

	result, err := h.words.List(c.Context(), filter, page)
	if err != nil {
		return err
	}

	return lib.Paginated(c, models.Page[serializers.WordResponse]{
		Items: serializers.ToWordResponses(result.Items),
		Meta:  result.Meta,
	})
}
