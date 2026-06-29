package models

// Artwork представляет одну работу из архива.
// Поля и их типы зеркалят то, что реально отдаёт sheets-export / batch.sh
// в artworks.json — НЕ идеализированную схему из conventions.md.
//
// Важные расхождения с conventions.md, на которые стоит обратить внимание:
//   - первичный ключ в данных называется "uuid", а не "id"
//   - tags — это []string, а не строка через запятую
//   - year и dimensions остаются string (нормализация — задача Фазы 5/Database)
type Artwork struct {
	UUID       string   `json:"uuid"`
	Filename   string   `json:"filename"`
	S3URL      string   `json:"s3_url"`
	DriveURL   string   `json:"drive_url"`
	Title      string   `json:"title"`
	ArtistName string   `json:"artist_name"`
	Year       string   `json:"year"`
	YearCirca  bool     `json:"year_circa"`
	Medium     string   `json:"medium"`
	Dimensions string   `json:"dimensions"`
	Museum     string   `json:"museum"`
	Tags       []string `json:"tags"`
	Style      string   `json:"style"`
	Mood       string   `json:"mood"`
	SourceURL  string   `json:"source_url"`
	AddedDate  string   `json:"added_date"`
	Notes      string   `json:"notes"`
}

// Artist — агрегат для GET /artists: имя художника + количество работ в архиве.
type Artist struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Taxonomy — словарь допустимых значений, см. taxonomy.md.
// На старте Фазы 4 формируется из самих artworks.json (фактически
// встречающиеся значения), а не из отдельного экспорта листа taxonomy —
// это упрощение, задокументированное как открытый вопрос в architecture.md.
type Taxonomy struct {
	Style  []string `json:"style"`
	Mood   []string `json:"mood"`
	Museum []string `json:"museum"`
	Tags   []string `json:"tags"`
}
