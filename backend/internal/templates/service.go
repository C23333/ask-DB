package templates

import (
	"strings"
)

// Template 定义了一个可复用的 SQL 报表模版
type Template struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Keywords    []string          `json:"keywords"`
	SQL         string            `json:"sql"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	OwnerID     string            `json:"owner_id,omitempty"`
	IsSystem    bool              `json:"is_system"`
	Editable    bool              `json:"editable"`
}

// Service 负责管理模版和查询匹配
type Service struct {
	store *Store
}

// NewService creates a template service backed by store
func NewService(store *Store) *Service {
	svc := &Service{store: store}
	for _, tpl := range defaultTemplates() {
		rec := &Record{
			ID:          tpl.ID,
			Name:        tpl.Name,
			Description: tpl.Description,
			Keywords:    tpl.Keywords,
			SQL:         tpl.SQL,
			Parameters:  tpl.Parameters,
			IsSystem:    true,
		}
		_ = store.Save(rec)
	}
	return svc
}

// Match 根据用户查询返回最合适的模版
func (s *Service) Match(query string) *Template {
	list, err := s.store.ListAll()
	if err != nil {
		return nil
	}
	normalized := strings.ToLower(query)
	score := 0
	var best *Template
	for _, rec := range list {
		current := matchScore(normalized, rec.Keywords)
		if current > score {
			copy := templateFromRecord(rec, "")
			best = &copy
			score = current
		}
	}
	if score < 2 {
		return nil
	}
	return best
}

// ListForUser returns templates with editable flag for given user
func (s *Service) ListForUser(userID string) ([]Template, error) {
	list, err := s.store.ListAll()
	if err != nil {
		return nil, err
	}
	result := make([]Template, 0, len(list))
	for _, rec := range list {
		result = append(result, templateFromRecord(rec, userID))
	}
	return result, nil
}

func (s *Service) SaveTemplate(tpl *Template) error {
	rec := &Record{
		ID:          tpl.ID,
		Name:        tpl.Name,
		Description: tpl.Description,
		Keywords:    tpl.Keywords,
		SQL:         tpl.SQL,
		Parameters:  tpl.Parameters,
		OwnerID:     tpl.OwnerID,
		IsSystem:    tpl.IsSystem,
	}
	return s.store.Save(rec)
}

func (s *Service) DeleteTemplate(id string) error {
	return s.store.Delete(id)
}

func (s *Service) GetTemplate(id string, userID string) (*Template, error) {
	rec, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	tpl := templateFromRecord(rec, userID)
	return &tpl, nil
}

func templateFromRecord(rec *Record, userID string) Template {
	editable := !rec.IsSystem && rec.OwnerID == userID
	return Template{
		ID:          rec.ID,
		Name:        rec.Name,
		Description: rec.Description,
		Keywords:    rec.Keywords,
		SQL:         rec.SQL,
		Parameters:  rec.Parameters,
		OwnerID:     rec.OwnerID,
		IsSystem:    rec.IsSystem,
		Editable:    editable,
	}
}

func matchScore(query string, keywords []string) int {
	hits := 0
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(query, kw) {
			hits++
		}
	}
	return hits
}

func defaultTemplates() []Template {
	return []Template{
		{
			ID:          "store_recent_activity",
			Name:        "门店近30天新建记录",
			Description: "列出最近 30 天内新增的门店及其基础信息",
			Keywords:    []string{"门店", "最近30天", "报表", "store", "新增"},
			SQL: `SELECT VC2_STORE_CODE,
       VC2STORE_FULL_NAME,
       VC2AREA,
       VC2STATUS,
       CREATE_TIME
FROM (
    SELECT VC2_STORE_CODE,
           VC2STORE_FULL_NAME,
           VC2AREA,
           VC2STATUS,
           CREATE_TIME
    FROM SY2025.MM_STORE
    WHERE CREATE_TIME >= SYSDATE - 30
    ORDER BY CREATE_TIME DESC
)
WHERE ROWNUM <= 50`,
			Parameters: map[string]string{
				"days":   "30",
				"limit":  "50",
				"schema": "SY2025",
			},
			IsSystem: true,
		},
	}
}
