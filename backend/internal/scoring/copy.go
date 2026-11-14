package scoring

import (
	"hash/fnv"

	"github.com/said-go/vibe-check/backend/internal/analysis"
)

// These are playful captions for the detected style, not claims about a person.
// Specific visible details belong in the evidence, not in a random caption.
var categorySummaries = map[string][]string{
	"action_hero": {
		"Кадр для афиши готов. Взрывы дорисуют на монтаже.",
		"Похоже на постер боевика. Название фильма придумаем после обеда.",
		"Кинообраз собран. Осталось включить музыку из трейлера.",
		"Обычное фото попросило бюджет на спецэффекты.",
	},
	"muscular": {
		"Мышцы попали в кадр раньше, чем успели сказать «сыр».",
		"У этого кадра есть рельеф. И это не фон.",
		"Мышцы решили, что сегодня они главный аксессуар.",
		"Хотели посмотреть на образ. Мышцы попросили отдельный кадр.",
	},
	"urban_classic": {
		"Образ будто на встречу. Даже если встреча — с шаурмой.",
		"У этого образа в календаре явно есть «важные дела».",
		"В таком образе даже за хлебом — почти деловой визит.",
		"Одежда уже готова к встрече. Осталось придумать повестку.",
	},
	"healthy_casual": {
		"Вещи подружились. Семейный чат можно не созывать.",
		"Образ на каждый день. Зеркало отпустило без замечаний.",
		"План простой: одеться и выйти. Кажется, сработало.",
		"Повседневный образ без лишней суеты. Можно идти по делам.",
	},
	"sport_chic": {
		"И за кофе, и на тренировку. Образ готов к обоим планам.",
		"Спортивный режим включён. Дойти до холодильника тоже считается.",
		"Образ на низком старте. Куда бежать — решим по дороге.",
		"Одежда за движение. Диван пока воздержался.",
	},
	"streetwear": {
		"Город — подиум, маршрутка — первый ряд.",
		"Уличный образ на выход. Даже если выход — до магазина.",
		"Прогулка обычная, образ — со своей программой.",
		"У этого выхода во двор есть свой дресс-код.",
	},
	"alternative": {
		"У образа своя программа. Пункт «как у всех» пропущен.",
		"Образ вышел за рамки. Рамки остались дома.",
		"Обычный дресс-код сегодня получил выходной.",
		"В этом образе есть свой поворот. Навигатор пересчитывает маршрут.",
	},
	"goth": {
		"У образа тёмная тема. Светлая версия пока не вышла.",
		"Готический режим включён. Даже гардероб слушает внимательно.",
		"Образ будто с обложки альбома. Название придумаем потом.",
		"У этого образа свой мрачный саундтрек. Наушники необязательны.",
	},
	"retro_90s": {
		"Образ как со старой видеокассеты. Только перемотки не хватает.",
		"Ретро-режим включён. Сейчас бы ещё дату в углу кадра.",
		"Девяностые передали привет через гардероб.",
		"Кажется, этот образ помнит кассеты. Даже если фото снято сегодня.",
	},
	"mula": {
		"Тюбетейка и борода попали в кадр. Название для образа нашлось быстро.",
		"Тюбетейка на месте, борода тоже. Образ собрал свой комплект.",
		"Тюбетейка с бородой взяли главные роли. Остальной гардероб — в титрах.",
		"Тюбетейка и борода — две детали, а уже целый образ.",
	},
	"clown": {
		"Гримаса перехватила эфир. Одежда сегодня в массовке.",
		"Хотели оценить одежду, но лицо устроило отдельное шоу.",
		"Гримаса уже в главной роли. Гардероб ждёт следующую серию.",
		"Кадр планировался обычным, но гримаса внесла правки.",
	},
	"cool_pose": {
		"Палец вверх, лицо серьёзное. Осталось придумать важную мысль.",
		"Палец поднят — внимание на экран. Сейчас будет объявление.",
		"С таким серьёзным лицом и поднятым пальцем даже меню звучит как речь.",
		"Палец уже поднят, серьёзный вид готов. Микрофон пока не выдали.",
	},
	"pupsik": {
		"Улыбка, наклон головы — у кадра уже плюс один лайк.",
		"Улыбка и мягкая поза без спецэффектов. Фильтр сегодня отдыхает.",
		"Улыбка и наклон головы готовы. Можно уже не говорить «сыр».",
		"Улыбка вместе с позой забрала внимание. Одежда не возражает.",
	},
}

func summaryForCategory(code string, f analysis.VisualFeatures) string {
	// Keep this helper safe even if called with inconsistent category data.
	if (code == "action_hero" && !f.HasActionHeroCombination()) || (code == "muscular" && !f.MuscularVisible) || (code == "mula" && !(f.SkullcapVisible && f.BeardVisible)) ||
		(code == "clown" && !f.PlayfulGrimace) ||
		(code == "cool_pose" && !f.SeriousIndexPose) ||
		(code == "pupsik" && !f.HasGentlePose()) {
		return "Смотрим по деталям, которые попали в кадр."
	}
	variants := categorySummaries[code]
	if len(variants) == 0 {
		return "Смотрим по деталям, которые попали в кадр."
	}
	// Stable for identical evidence. Confidence, compression quality and crop
	// visibility do not pick the joke; they get separate uncertainty text.
	hash := fnv.New32a()
	_, _ = hash.Write([]byte{
		byte(f.ColorHarmony), byte(f.Darkness), byte(f.Formality), byte(f.Sportiness),
		byte(f.Originality), byte(f.OutfitCoherence), byte(f.AccessoryIntensity), byte(f.Layering),
		byte(f.Signals.Goth), byte(f.Signals.Alternative), byte(f.Signals.Streetwear),
		byte(f.Signals.Classic), byte(f.Signals.SportChic), byte(f.Signals.Retro90s),
	})
	return variants[int(hash.Sum32()%uint32(len(variants)))]
}

func recommendationsForFeatures(f analysis.VisualFeatures) []string {
	// A cropped or uncertain picture is not evidence that the whole outfit
	// needs changing. Help with the picture before suggesting clothing edits.
	if f.OutfitVisibility < 60 {
		items := []string{"Хочешь оценить весь образ — поставь камеру чуть дальше"}
		if f.ImageQuality < 55 {
			items = append(items, "Для более чёткого кадра попробуй встать ближе к свету")
		}
		return items
	}
	if f.ImageQuality < 55 || f.Confidence < 70 {
		return []string{"Для более точного результата попробуй другой ракурс при хорошем свете"}
	}
	items := make([]string, 0, 2)
	if f.ColorHarmony < 65 {
		items = append(items, "Попробуй оставить два-три основных цвета")
	}
	if f.OutfitCoherence < 70 {
		items = append(items, "Попробуй повторить один цвет в двух вещах")
	}
	if len(items) < 2 && f.AccessoryIntensity < 35 {
		items = append(items, "Если хочется добавить деталь — попробуй один аксессуар")
	}
	if len(items) == 0 {
		items = append(items, "Можно оставить как есть — вещи уже сочетаются")
	}
	return items
}
