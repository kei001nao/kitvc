package db

import (
	"encoding/json"
	"fmt"
	"strings"
)

type filterRule struct {
	Field string      `json:"field"`
	Op    string      `json:"op"`
	Value interface{} `json:"value"`
}

type filterGroup struct {
	Op    string        `json:"op"`
	Rules []interface{} `json:"rules"`
}

type filterSortItem []interface{}

func GetFilteredMusicTracks(conditionsJSON, sortJSON string) ([]TrackData, error) {
	var conditions interface{}
	if conditionsJSON != "" {
		json.Unmarshal([]byte(conditionsJSON), &conditions)
	}

	var sortFields []filterSortItem
	if sortJSON != "" {
		json.Unmarshal([]byte(sortJSON), &sortFields)
	}

	whereClause, params := buildWhereClause(conditions)

	query := "SELECT path, mtime, title, artist, album, album_artist, track_num, disc_num, genre, duration FROM music_tracks"
	if whereClause != "" {
		query += " WHERE " + whereClause
	}

	if len(sortFields) > 0 {
		var orderParts []string
		for _, item := range sortFields {
			if len(item) >= 1 {
				field := fmt.Sprintf("%v", item[0])
				if !isSafeFieldName(field) {
					continue
				}
				direction := "ASC"
				if len(item) >= 2 {
					dir := fmt.Sprintf("%v", item[1])
					if strings.EqualFold(dir, "DESC") {
						direction = "DESC"
					}
				}
				orderParts = append(orderParts, field+" COLLATE NOCASE "+direction)
			}
		}
		if len(orderParts) > 0 {
			query += " ORDER BY " + strings.Join(orderParts, ", ")
		}
	} else {
		query += " ORDER BY artist, album, disc_num, track_num, title"
	}

	rows, err := db.Query(query, params...)
	if err != nil {
		return nil, fmt.Errorf("filter query failed: %w", err)
	}
	defer rows.Close()

	var tracks []TrackData
	for rows.Next() {
		var t TrackData
		if err := rows.Scan(&t.Path, &t.MTime, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.TrackNum, &t.DiscNum, &t.Genre, &t.Duration); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func buildWhereClause(data interface{}) (string, []interface{}) {
	if data == nil {
		return "", nil
	}

	switch v := data.(type) {
	case []interface{}:
		if len(v) == 0 {
			return "", nil
		}
		return buildGroupClause("AND", v)
	case map[string]interface{}:
		op, _ := v["op"].(string)
		rules, _ := v["rules"].([]interface{})
		if op == "" {
			op = "AND"
		}
		return buildGroupClause(op, rules)
	}
	return "", nil
}

func buildGroupClause(op string, rules []interface{}) (string, []interface{}) {
	if len(rules) == 0 {
		return "", nil
	}

	var parts []string
	var allParams []interface{}

	for _, rule := range rules {
		switch r := rule.(type) {
		case map[string]interface{}:
			if _, hasOp := r["op"]; hasOp {
				if _, hasRules := r["rules"]; hasRules {
					subWhere, subParams := buildWhereClause(r)
					if subWhere != "" {
						parts = append(parts, "("+subWhere+")")
						allParams = append(allParams, subParams...)
					}
					continue
				}
			}
			field, _ := r["field"].(string)
			operator, _ := r["op"].(string)
			value := r["value"]

			if field == "" || !isSafeFieldName(field) {
				continue
			}
			if operator == "" {
				operator = "=="
			}

			sqlClause, param := buildLeafClause(field, operator, value)
			if sqlClause != "" {
				parts = append(parts, sqlClause)
				if param != nil {
					allParams = append(allParams, param)
				}
			}
		}
	}

	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, " "+strings.ToUpper(op)+" "), allParams
}

func buildLeafClause(field, operator string, value interface{}) (string, interface{}) {
	// Use COALESCE to handle NULL values properly
	coalesceField := "COALESCE(" + field + ", '')"
	
	switch operator {
	case "==":
		// For empty string, match both NULL and empty string
		if value == "" || value == nil {
			return "(" + field + " IS NULL OR " + coalesceField + " = ?)", ""
		}
		return coalesceField + " = ?", value
	case "!=":
		// For empty string, exclude both NULL and empty string
		if value == "" || value == nil {
			return "(" + field + " IS NOT NULL AND " + coalesceField + " != ?)", ""
		}
		return "(" + coalesceField + " != ? OR " + field + " IS NULL)", value
	case "contains":
		return coalesceField + " LIKE ?", "%" + fmt.Sprintf("%v", value) + "%"
	case "not_contains":
		return coalesceField + " NOT LIKE ?", "%" + fmt.Sprintf("%v", value) + "%"
	case ">":
		// For numeric comparison, need to handle non-numeric values
		return "(" + field + " IS NOT NULL AND " + field + " > ?)", value
	case "<":
		return "(" + field + " IS NOT NULL AND " + field + " < ?)", value
	case ">=":
		return "(" + field + " IS NOT NULL AND " + field + " >= ?)", value
	case "<=":
		return "(" + field + " IS NOT NULL AND " + field + " <= ?)", value
	case "is_null":
		return field + " IS NULL", nil
	case "is_not_null":
		return field + " IS NOT NULL", nil
	}
	return "", nil
}

func isSafeFieldName(field string) bool {
	if field == "" {
		return false
	}
	for _, c := range field {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}
