package handler

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Blankon-Developer/be-pawfund/internal/domain"
	"github.com/Blankon-Developer/be-pawfund/internal/http/httpx"
	"github.com/Blankon-Developer/be-pawfund/internal/service"
	"github.com/google/uuid"
)

const (
	defaultCampaignListPage          int64 = 1
	defaultCampaignListPageSize      int64 = 10
	maxCampaignListPageSize          int64 = 100
	defaultCampaignDonorListPage     int64 = 1
	defaultCampaignDonorListPageSize int64 = 10
	maxCampaignDonorListPageSize     int64 = 100
)

type createCampaignRequest struct {
	Title            string `json:"title"`
	ShortDescription string `json:"shortDescription"`
	Story            string `json:"story"`
	GoalAmount       int64  `json:"goalAmount"`
	EndAt            string `json:"endAt"`
	ImageObjectKey   string `json:"imageObjectKey"`
	Country          string `json:"country"`
	ZipCode          string `json:"zipCode"`
}

func (r *createCampaignRequest) normalize() {
	r.Title = strings.TrimSpace(r.Title)
	r.ShortDescription = strings.TrimSpace(r.ShortDescription)
	r.Story = strings.TrimSpace(r.Story)
	r.EndAt = strings.TrimSpace(r.EndAt)
	r.ImageObjectKey = strings.TrimSpace(r.ImageObjectKey)
	r.Country = strings.TrimSpace(r.Country)
	r.ZipCode = strings.TrimSpace(r.ZipCode)
}

func (r createCampaignRequest) validate() (httpx.FieldErrors, time.Time) {
	fieldErrors := make(httpx.FieldErrors)
	validateRequiredLength(fieldErrors, "title", r.Title, maxNameCharacters)
	validateRequiredLength(fieldErrors, "shortDescription", r.ShortDescription, maxNameCharacters)
	if r.Story == "" {
		fieldErrors.Add("story", "story is required!")
	}
	if r.GoalAmount <= 0 {
		fieldErrors.Add("goalAmount", "goalAmount must be greater than 0!")
	}

	var endAt time.Time
	if r.EndAt == "" {
		fieldErrors.Add("endAt", "endAt is required!")
	} else {
		parsed, err := time.Parse(time.RFC3339, r.EndAt)
		if err != nil {
			fieldErrors.Add("endAt", "endAt must be a valid RFC3339 timestamp!")
		} else {
			endAt = parsed.UTC()
		}
	}

	if r.ImageObjectKey == "" {
		fieldErrors.Add("imageObjectKey", "imageObjectKey is required!")
	} else {
		validateStagedImageObjectKey(fieldErrors, r.ImageObjectKey, service.CampaignImageDirectory)
	}
	validateRequiredLength(fieldErrors, "country", r.Country, maxCountryCharacters)
	validateRequiredLength(fieldErrors, "zipCode", r.ZipCode, maxZipCodeCharacters)

	if !utf8.ValidString(r.Story) {
		fieldErrors.Add("story", "story must contain valid UTF-8 text!")
	}
	if len(fieldErrors) == 0 {
		return nil, endAt
	}
	return fieldErrors, time.Time{}
}

type createCampaignResponse struct {
	ID               string                `json:"id"`
	Title            string                `json:"title"`
	ShortDescription string                `json:"shortDescription"`
	Story            string                `json:"story"`
	GoalAmount       int64                 `json:"goalAmount"`
	RaisedAmount     int64                 `json:"raisedAmount"`
	DonorCount       int64                 `json:"donorCount"`
	EndAt            string                `json:"endAt"`
	ImageURL         *string               `json:"imageUrl"`
	Country          string                `json:"country"`
	ZipCode          string                `json:"zipCode"`
	Status           domain.CampaignStatus `json:"status"`
	ContractAddress  *string               `json:"contractAddress"`
	CreatedAt        string                `json:"createdAt"`
}

type myCampaignResponse struct {
	Title            string                `json:"title"`
	ShortDescription string                `json:"shortDescription"`
	Story            string                `json:"story"`
	GoalAmount       int64                 `json:"goalAmount"`
	RaisedAmount     int64                 `json:"raisedAmount"`
	DonorCount       int64                 `json:"donorCount"`
	EndAt            string                `json:"endAt"`
	ImageURL         *string               `json:"imageUrl"`
	Country          string                `json:"country"`
	ZipCode          string                `json:"zipCode"`
	Status           domain.CampaignStatus `json:"status"`
	ContractAddress  *string               `json:"contractAddress"`
	CreatedAt        string                `json:"createdAt"`
}

type myCampaignListItemResponse struct {
	ID               uuid.UUID             `json:"id"`
	Title            string                `json:"title"`
	ShortDescription string                `json:"shortDescription"`
	GoalAmount       int64                 `json:"goalAmount"`
	RaisedAmount     int64                 `json:"raisedAmount"`
	DonorCount       int64                 `json:"donorCount"`
	ImageURL         *string               `json:"imageUrl"`
	EndAt            string                `json:"endAt"`
	CreatedAt        string                `json:"createdAt"`
	ContractAddress  *string               `json:"contractAddress"`
	Status           domain.CampaignStatus `json:"status"`
}

var (
	publicCampaignListStatuses = []domain.CampaignStatus{
		domain.CampaignStatusActive,
		domain.CampaignStatusCompleted,
		domain.CampaignStatusCancelled,
	}
	fundraiserCampaignListStatuses = []domain.CampaignStatus{
		domain.CampaignStatusPending,
		domain.CampaignStatusActive,
		domain.CampaignStatusCompleted,
		domain.CampaignStatusCancelled,
		domain.CampaignStatusFailed,
	}
)

func campaignListOptionsFromQuery(query url.Values, allowedStatuses []domain.CampaignStatus) (domain.CampaignListOptions, httpx.FieldErrors) {
	options := domain.CampaignListOptions{
		Search:   strings.TrimSpace(query.Get("search")),
		Sort:     domain.CampaignListSortNewest,
		Page:     defaultCampaignListPage,
		PageSize: defaultCampaignListPageSize,
	}
	fieldErrors := make(httpx.FieldErrors)

	if rawSort, ok := query["sortBy"]; ok {
		switch sort := strings.TrimSpace(firstQueryValue(rawSort)); sort {
		case "", string(domain.CampaignListSortNewest):
			options.Sort = domain.CampaignListSortNewest
		case string(domain.CampaignListSortOldest):
			options.Sort = domain.CampaignListSortOldest
		case string(domain.CampaignListSortCloseToGoal):
			options.Sort = domain.CampaignListSortCloseToGoal
		case string(domain.CampaignListSortMostDonated):
			options.Sort = domain.CampaignListSortMostDonated
		default:
			fieldErrors.Add("sortBy", "sortBy must be one of newest, oldest, close-to-goal, or most-donated!")
		}
	}

	if rawFilter, ok := query["filter"]; ok {
		filter := strings.TrimSpace(firstQueryValue(rawFilter))
		if filter != "" {
			if status, ok := campaignStatusFromAllowed(filter, allowedStatuses); ok {
				options.Status = &status
			} else {
				fieldErrors.Add("filter", campaignListFilterError(allowedStatuses))
			}
		}
	}

	if rawPage, ok := query["page"]; ok {
		page, valid := parsePositiveInt64(firstQueryValue(rawPage))
		if !valid {
			fieldErrors.Add("page", "page must be a positive integer!")
		} else {
			options.Page = page
		}
	}

	if rawPageSize, ok := query["pageSize"]; ok {
		pageSize, valid := parsePositiveInt64(firstQueryValue(rawPageSize))
		if !valid || pageSize > maxCampaignListPageSize {
			fieldErrors.Add("pageSize", "pageSize must be an integer between 1 and 100!")
		} else {
			options.PageSize = pageSize
		}
	}

	if _, pageInvalid := fieldErrors["page"]; !pageInvalid && options.Page-1 > math.MaxInt64/options.PageSize {
		fieldErrors.Add("page", "page is too large!")
	}

	if len(fieldErrors) == 0 {
		return options, nil
	}
	return domain.CampaignListOptions{}, fieldErrors
}

func campaignStatusFromAllowed(filter string, allowed []domain.CampaignStatus) (domain.CampaignStatus, bool) {
	for _, status := range allowed {
		if filter == string(status) {
			return status, true
		}
	}
	return "", false
}

func campaignListFilterError(allowed []domain.CampaignStatus) string {
	if len(allowed) == 0 {
		return "filter is invalid!"
	}
	if len(allowed) == 1 {
		return "filter must be " + string(allowed[0]) + "!"
	}
	names := make([]string, 0, len(allowed))
	for _, status := range allowed {
		names = append(names, string(status))
	}
	if len(names) == 2 {
		return "filter must be one of " + names[0] + " or " + names[1] + "!"
	}
	return "filter must be one of " + strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1] + "!"
}

func firstQueryValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func parsePositiveInt64(value string) (int64, bool) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 1 {
		return 0, false
	}
	return parsed, true
}

type publicCampaignListItemResponse struct {
	ID                 uuid.UUID             `json:"id"`
	Title              string                `json:"title"`
	ShortDescription   string                `json:"shortDescription"`
	GoalAmount         int64                 `json:"goalAmount"`
	RaisedAmount       int64                 `json:"raisedAmount"`
	DonorCount         int64                 `json:"donorCount"`
	CampaignImageURL   *string               `json:"campaignImageUrl"`
	FundraiserImageURL *string               `json:"fundraiserImageUrl"`
	EndAt              string                `json:"endAt"`
	CreatedAt          string                `json:"createdAt"`
	ContractAddress    *string               `json:"contractAddress"`
	Status             domain.CampaignStatus `json:"status"`
}

type campaignFundraiser struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	ImageURL *string   `json:"imageUrl"`
	Address  string    `json:"address"`
}

type publicCampaignDetailResponse struct {
	ID               uuid.UUID             `json:"id"`
	Title            string                `json:"title"`
	ShortDescription string                `json:"shortDescription"`
	Story            string                `json:"story"`
	Fundraiser       campaignFundraiser    `json:"fundraiser"`
	GoalAmount       int64                 `json:"goalAmount"`
	RaisedAmount     int64                 `json:"raisedAmount"`
	DonorCount       int64                 `json:"donorCount"`
	ContractAddress  string                `json:"contractAddress"`
	EndAt            string                `json:"endAt"`
	CreatedAt        string                `json:"createdAt"`
	ImageURL         string                `json:"imageUrl"`
	Country          string                `json:"country"`
	ZipCode          string                `json:"zipCode"`
	Status           domain.CampaignStatus `json:"status"`
}

type publicCampaignDonorsItemResponse struct {
	Name      *string `json:"name"`
	Address   string  `json:"address"`
	ImageURL  *string `json:"imageUrl"`
	Amount    int64   `json:"amount"`
	DonatedOn string  `json:"donatedOn"`
}

func campaignDonorListOptionsFromQuery(query url.Values) (domain.CampaignDonorListOptions, httpx.FieldErrors) {
	options := domain.CampaignDonorListOptions{
		Sort:     domain.CampaignDonorListSortRecent,
		Page:     defaultCampaignDonorListPage,
		PageSize: defaultCampaignDonorListPageSize,
	}
	fieldErrors := make(httpx.FieldErrors)

	if rawSort, ok := query["sortBy"]; ok {
		switch sort := strings.TrimSpace(firstQueryValue(rawSort)); sort {
		case "", string(domain.CampaignDonorListSortRecent):
			options.Sort = domain.CampaignDonorListSortRecent
		case string(domain.CampaignDonorListSortTop):
			options.Sort = domain.CampaignDonorListSortTop
		default:
			fieldErrors.Add("sortBy", "sortBy must be one of recent or top!")
		}
	}

	if rawPage, ok := query["page"]; ok {
		page, valid := parsePositiveInt64(firstQueryValue(rawPage))
		if !valid {
			fieldErrors.Add("page", "page must be a positive integer!")
		} else {
			options.Page = page
		}
	}

	if rawPageSize, ok := query["pageSize"]; ok {
		pageSize, valid := parsePositiveInt64(firstQueryValue(rawPageSize))
		if !valid || pageSize > maxCampaignDonorListPageSize {
			fieldErrors.Add("pageSize", "pageSize must be an integer between 1 and 100!")
		} else {
			options.PageSize = pageSize
		}
	}

	if _, pageInvalid := fieldErrors["page"]; !pageInvalid && options.Page-1 > math.MaxInt64/options.PageSize {
		fieldErrors.Add("page", "page is too large!")
	}

	if len(fieldErrors) == 0 {
		return options, nil
	}
	return domain.CampaignDonorListOptions{}, fieldErrors
}
