package api

import (
	"github.com/nagare-project/nagare/internal/animego"
	"html"
	"math"
	"regexp"
	"strings"
)

var descriptionTags = regexp.MustCompile(`<[^>]*>`)
var descriptionScripts = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(?:script|style)>`)
var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func firstText(values ...string) string {
	for _, s := range values {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
func plainDescription(s string) string {
	return strings.TrimSpace(html.UnescapeString(descriptionTags.ReplaceAllString(descriptionScripts.ReplaceAllString(s, ""), "")))
}
func (s *DiscoverService) summary(r animego.CatalogMedia) SummaryMedia {
	m := SummaryMedia{AnilistID: r.AnilistID, Title: firstText(r.TitleChinese, r.TitleRomaji, r.TitleNative, r.TitleEnglish, r.Title), TitleNative: r.TitleNative, TitleEnglish: r.TitleEnglish,
		Cover: s.art.Register(r.CoverImageURL), Banner: s.art.Register(r.BannerImageURL), Year: r.SeasonYear, Season: map[string]string{"WINTER": "冬", "SPRING": "春", "SUMMER": "夏", "FALL": "秋"}[r.Season], Episodes: r.Episodes, Score: int(math.Round(r.AverageScore)), Genres: append([]string{}, r.Genres...), Description: plainDescription(firstText(r.DescriptionCn, r.Description)), Status: r.Status, Format: r.Format, Duration: r.Duration, Source: r.Source, StartDate: r.StartDate, Studios: append([]string{}, r.Studios...)}
	id, site := r.TrailerID, r.TrailerSite
	if r.Trailer != nil {
		id, site = r.Trailer.ID, r.Trailer.Site
	}
	if strings.EqualFold(site, "youtube") && youtubeID.MatchString(id) {
		m.TrailerID = id
	}
	for _, rel := range r.Relations {
		if rel.AnilistID > 0 && rel.RelationType != "CHARACTER" && rel.Format != "MANGA" && rel.Format != "NOVEL" && rel.Format != "ONE_SHOT" {
			item := s.summary(rel.CatalogMedia)
			if item.Title != "" {
				m.Relations = append(m.Relations, Relation{rel.RelationType, item})
			}
		}
	}
	for _, r := range r.Recommendations {
		item := s.summary(r)
		if item.AnilistID > 0 && item.Title != "" {
			m.Recommendations = append(m.Recommendations, item)
		}
	}
	for _, c := range r.Characters {
		m.Characters = append(m.Characters, Character{firstText(c.NameCn, c.NameJa, c.NameEn), s.art.Register(c.ImageURL), c.Role, firstText(c.VoiceActorCn, c.VoiceActorJa, c.VoiceActorEn), s.art.Register(c.VoiceActorImageURL)})
	}
	for _, ep := range r.EpisodeTitles {
		if ep.Episode > 0 {
			m.EpisodeTitles = append(m.EpisodeTitles, EpisodeTitleView{ep.Episode, firstText(ep.NameCn, ep.Name)})
		}
	}
	return m
}
func (s *DiscoverService) summaries(rows []animego.CatalogMedia) []SummaryMedia {
	out := []SummaryMedia{}
	seen := map[int]bool{}
	for _, r := range rows {
		m := s.summary(r)
		if m.AnilistID > 0 && m.Title != "" && !seen[m.AnilistID] {
			seen[m.AnilistID] = true
			out = append(out, m)
		}
	}
	return out
}
