(function () {
  const categories = [
    { code: 'urban_classic', title: 'Строгий городской', description: 'Собранный городской образ: спокойно, уверенно и без лишнего шума.', weights: { coherence: .35, contrast: .15, darkness: .15, restraint: .35 } },
    { code: 'healthy_casual', title: 'Здравый casual', description: 'Удобный и понятный образ, который выглядит уверенно без попытки что-то доказать.', weights: { coherence: .4, balance: .3, restraint: .2, brightness: .1 } },
    { code: 'sport_chic', title: 'Спорт-шик', description: 'Динамичный образ, в котором комфорт работает вместе с заметными деталями.', weights: { contrast: .3, saturation: .2, coherence: .3, brightness: .2 } },
    { code: 'streetwear', title: 'Streetwear', description: 'Городская энергия, контрасты и ощущение, что образ собран по своим правилам.', weights: { contrast: .4, saturation: .25, originality: .35 } },
    { code: 'alternative', title: 'Нефор', description: 'Нестандартный образ с выразительными деталями и собственным визуальным языком.', weights: { originality: .5, contrast: .25, saturation: .25 } },
    { code: 'goth', title: 'Гот', description: 'Тёмная палитра, драматичный контраст и собранная альтернативная эстетика.', weights: { darkness: .5, contrast: .3, coherence: .2 } }
  ];
  const levels = [
    { min: 0, code: 'experimenter', title: 'ЭКСПЕРИМЕНТАТОР' },
    { min: 50, code: 'base', title: 'БАЗОВЫЙ ВАЙБ' },
    { min: 65, code: 'healthy', title: 'ЗДРАВЫЙ ОБРАЗ' },
    { min: 80, code: 'styler', title: 'СТИЛЯГА' },
    { min: 92, code: 'icon', title: 'ИКОНА ВАЙБА' }
  ];
  const scoreCategory = (category, features) => Object.entries(category.weights).reduce((sum, [key, weight]) => sum + (features[key] || 0) * weight, 0);
  window.VIBE_TAXONOMY = Object.freeze({
    version: '1.0.0', categories, levels,
    classify(features) {
      const ranked = categories.map(category => ({ ...category, score: Math.round(scoreCategory(category, features)) })).sort((a, b) => b.score - a.score);
      return { primary: ranked[0], secondary: ranked[1], ranked };
    },
    levelFor(score) { return [...levels].reverse().find(level => score >= level.min) || levels[0]; }
  });
})();
