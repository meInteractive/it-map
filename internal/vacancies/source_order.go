package vacancies

import "sort"

// Исходные SQLite-чтения обходят vacancy/channel index; первый канал виден в UI.
// Сортируем собственную копию DTO, сохраняя source.id порядок сырых provenance.
func orderedTelegramSources(input []TelegramSource) []TelegramSource {
	out := append([]TelegramSource{}, input...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ChannelID < out[j].ChannelID })
	return out
}

func orderedDetailTelegramSources(input []DetailTelegramSource) []DetailTelegramSource {
	out := append([]DetailTelegramSource{}, input...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ChannelID < out[j].ChannelID })
	return out
}
