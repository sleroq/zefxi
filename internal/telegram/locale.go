package telegram

import "strings"

type locale struct {
	reactsReply, linkReply, linkLoadError, authorUnknown                                                        string
	discordUser, notLinked, linkedTelegram, none, telegramUser, selectAccount, linkedMessages                   string
	unlink, add, previous, next                                                                                 string
	whoisReply, unavailable, unauthorized, pageError, unlinked, linked, changeError, refreshError, displayError string
	notDiscordMessage, noDiscordReactions, discordReactions                                                     string
}

var english = locale{
	reactsReply: "Reply to a message with /reacts to see its reactions.", linkReply: "Reply to a message, including a bridged Discord message, with /link [username or ID].", linkLoadError: "Unable to load link choices. Please try again later.", authorUnknown: "The author could not be identified.",
	discordUser: "Discord user: ", notLinked: "Not linked", linkedTelegram: "Linked telegram users:", none: "None", telegramUser: "- telegram: ", selectAccount: "Select an account to add or remove a link.", linkedMessages: " Messages sent from linked accounts will show up on discord as messages sent by %s",
	unlink: "Unlink ", add: "Add ", previous: "← Previous", next: "Next →",
	whoisReply: "Reply to a message with /whois to identify its author.", unavailable: "This link choice is no longer available.", unauthorized: "Only the person who requested this link can choose.", pageError: "Unable to change page.", unlinked: "Users unlinked.", linked: "Users linked.", changeError: "Unable to change link. Please try again.", refreshError: "Link changed, but choices could not be refreshed. Please use /link again.", displayError: "Link changed, but display could not be refreshed. Please use /link again.",
	notDiscordMessage: "This message is not linked to a Discord message.", noDiscordReactions: "No Discord reactions.", discordReactions: "Discord reactions:",
}
var russian = locale{
	reactsReply: "Ответьте на сообщение командой /reacts, чтобы увидеть реакции.", linkReply: "Ответьте на сообщение, в том числе пересланное из Discord, командой /link [имя пользователя или ID].", linkLoadError: "Не удалось загрузить варианты привязки. Попробуйте позже.", authorUnknown: "Не удалось определить автора.",
	discordUser: "Пользователь Discord: ", notLinked: "Не привязан", linkedTelegram: "Привязанные пользователи Telegram:", none: "Нет", telegramUser: "- Telegram: ", selectAccount: "Выберите аккаунт, чтобы добавить или удалить привязку.", linkedMessages: " Сообщения из привязанных аккаунтов будут отображаться в Discord как сообщения от %s",
	unlink: "Отвязать ", add: "Добавить ", previous: "← Назад", next: "Далее →",
	whoisReply: "Ответьте на сообщение командой /whois, чтобы узнать его автора.", unavailable: "Этот вариант привязки больше недоступен.", unauthorized: "Выбор доступен только пользователю, запросившему привязку.", pageError: "Не удалось сменить страницу.", unlinked: "Пользователи отвязаны.", linked: "Пользователи привязаны.", changeError: "Не удалось изменить привязку. Попробуйте ещё раз.", refreshError: "Привязка изменена, но обновить варианты не удалось. Используйте /link ещё раз.", displayError: "Привязка изменена, но обновить отображение не удалось. Используйте /link ещё раз.",
	notDiscordMessage: "Это сообщение не связано с сообщением Discord.", noDiscordReactions: "Нет реакций Discord.", discordReactions: "Реакции Discord:",
}
var polish = locale{
	reactsReply: "Odpowiedz na wiadomość poleceniem /reacts, aby zobaczyć reakcje.", linkReply: "Odpowiedz na wiadomość, także przekazaną z Discorda, poleceniem /link [nazwa użytkownika lub ID].", linkLoadError: "Nie udało się załadować opcji powiązania. Spróbuj ponownie później.", authorUnknown: "Nie udało się ustalić autora.",
	discordUser: "Użytkownik Discorda: ", notLinked: "Niepowiązany", linkedTelegram: "Powiązani użytkownicy Telegrama:", none: "Brak", telegramUser: "- Telegram: ", selectAccount: "Wybierz konto, aby dodać lub usunąć powiązanie.", linkedMessages: " Wiadomości z powiązanych kont będą widoczne na Discordzie jako wysłane przez %s",
	unlink: "Odłącz ", add: "Dodaj ", previous: "← Poprzednia", next: "Następna →",
	whoisReply: "Odpowiedz na wiadomość poleceniem /whois, aby poznać jej autora.", unavailable: "Ta opcja powiązania nie jest już dostępna.", unauthorized: "Tylko osoba, która zażądała powiązania, może wybrać opcję.", pageError: "Nie udało się zmienić strony.", unlinked: "Użytkownicy zostali odłączeni.", linked: "Użytkownicy zostali powiązani.", changeError: "Nie udało się zmienić powiązania. Spróbuj ponownie.", refreshError: "Powiązanie zmieniono, ale nie udało się odświeżyć opcji. Użyj ponownie /link.", displayError: "Powiązanie zmieniono, ale nie udało się odświeżyć widoku. Użyj ponownie /link.",
	notDiscordMessage: "Ta wiadomość nie jest powiązana z wiadomością na Discordzie.", noDiscordReactions: "Brak reakcji na Discordzie.", discordReactions: "Reakcje na Discordzie:",
}
var ukrainian = locale{
	reactsReply: "Відповідайте на повідомлення командою /reacts, щоб побачити реакції.", linkReply: "Відповідайте на повідомлення, зокрема переслане з Discord, командою /link [ім'я користувача або ID].", linkLoadError: "Не вдалося завантажити варіанти прив'язки. Спробуйте пізніше.", authorUnknown: "Не вдалося визначити автора.",
	discordUser: "Користувач Discord: ", notLinked: "Не прив'язано", linkedTelegram: "Прив'язані користувачі Telegram:", none: "Немає", telegramUser: "- Telegram: ", selectAccount: "Виберіть акаунт, щоб додати або видалити прив'язку.", linkedMessages: " Повідомлення з прив'язаних акаунтів відображатимуться в Discord як повідомлення від %s",
	unlink: "Відв'язати ", add: "Додати ", previous: "← Назад", next: "Далі →",
	whoisReply: "Відповідайте на повідомлення командою /whois, щоб дізнатися його автора.", unavailable: "Цей варіант прив'язки більше недоступний.", unauthorized: "Вибирати може лише користувач, який запитав прив'язку.", pageError: "Не вдалося змінити сторінку.", unlinked: "Користувачів відв'язано.", linked: "Користувачів прив'язано.", changeError: "Не вдалося змінити прив'язку. Спробуйте ще раз.", refreshError: "Прив'язку змінено, але не вдалося оновити варіанти. Скористайтеся /link ще раз.", displayError: "Прив'язку змінено, але не вдалося оновити відображення. Скористайтеся /link ще раз.",
	notDiscordMessage: "Це повідомлення не пов'язане з повідомленням Discord.", noDiscordReactions: "Немає реакцій Discord.", discordReactions: "Реакції Discord:",
}

func language(code string) locale {
	base, _, _ := strings.Cut(strings.ToLower(strings.ReplaceAll(code, "_", "-")), "-")
	switch base {
	case "ru":
		return russian
	case "pl":
		return polish
	case "uk", "ua":
		return ukrainian
	default:
		return english
	}
}
