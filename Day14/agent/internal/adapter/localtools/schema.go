package localtools

import "ai-adv-agent/internal/domain"

func obj(props map[string]interface{}, required ...string) map[string]interface{} {
	s := map[string]interface{}{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var (
	str    = map[string]interface{}{"type": "string"}
	strArr = map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}
	num    = map[string]interface{}{"type": "number"}
	boolT  = map[string]interface{}{"type": "boolean"}
)

// Tools returns the local tool definitions. Names and schemas follow
// @modelcontextprotocol/server-filesystem, so the policy table applies as is.
func Tools() []domain.MCPTool {
	return []domain.MCPTool{
		{Name: "read_text_file", Description: "Прочитать текстовый файл (путь относительно корня). head/tail — первые/последние N строк.",
			InputSchema: obj(map[string]interface{}{"path": str, "head": num, "tail": num}, "path")},
		{Name: "read_multiple_files", Description: "Прочитать несколько файлов за один вызов.",
			InputSchema: obj(map[string]interface{}{"paths": strArr}, "paths")},
		{Name: "list_directory", Description: "Список файлов и каталогов ([FILE]/[DIR]).",
			InputSchema: obj(map[string]interface{}{"path": str}, "path")},
		{Name: "directory_tree", Description: "Рекурсивное дерево каталога в JSON.",
			InputSchema: obj(map[string]interface{}{"path": str}, "path")},
		{Name: "search_files", Description: "Найти файлы по имени (подстрока или glob) внутри каталога.",
			InputSchema: obj(map[string]interface{}{"path": str, "pattern": str}, "path", "pattern")},
		{Name: "get_file_info", Description: "Метаданные файла или каталога.",
			InputSchema: obj(map[string]interface{}{"path": str}, "path")},
		{Name: "write_file", Description: "Создать или полностью перезаписать файл.",
			InputSchema: obj(map[string]interface{}{"path": str, "content": str}, "path", "content")},
		{Name: "edit_file", Description: "Точечные правки: каждый oldText должен встречаться в файле ровно один раз (дословно, с отступами). dryRun — только показать diff.",
			InputSchema: obj(map[string]interface{}{
				"path": str,
				"edits": map[string]interface{}{"type": "array", "items": obj(map[string]interface{}{
					"oldText": str, "newText": str,
				}, "oldText", "newText")},
				"dryRun": boolT,
			}, "path", "edits")},
		{Name: "create_directory", Description: "Создать каталог (вместе с родителями).",
			InputSchema: obj(map[string]interface{}{"path": str}, "path")},
		{Name: "grep", Description: "Поиск по содержимому всех файлов корня (регулярное выражение RE2). Не более 200 совпадений.",
			InputSchema: obj(map[string]interface{}{"pattern": str, "caseSensitive": boolT}, "pattern")},
		{Name: "run_command", Description: "Выполнить команду в песочнице без сети и без шелла. argv — массив строк, например [\"go\",\"test\",\"./...\"]. Разрешены только команды профиля роли.",
			InputSchema: obj(map[string]interface{}{"argv": strArr}, "argv")},
	}
}
