package ai

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// normalizeVisionQuestionPayload 是供应商响应进入领域模型前的防腐层。
// Prompt 只能降低字段漂移概率；可恢复的 string/string[]、别名字段必须在这里统一，不能依赖模型重试碰运气。
func normalizeVisionQuestionPayload(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	diagram, _ := value["diagram"].(map[string]any)
	figures, _ := diagram["figures"].([]any)
	uncertainItems := flexibleStrings(value["uncertainItems"])
	for _, item := range figures {
		figure, ok := item.(map[string]any)
		if !ok {
			continue
		}
		uncertainItems = append(uncertainItems, normalizeVisionFigure(figure)...)
	}
	if len(uncertainItems) > 0 {
		value["uncertainItems"] = uniqueStrings(uncertainItems)
	}
	normalizeVisionExpressions(value)
	normalizeVisionRelations(value)
	normalizeVisionGrade(value)
	return json.Marshal(value)
}

// normalizeVisionGrade 只把明确的数字收敛为年级；“初中”等教育阶段描述无法唯一映射到年级时不猜测。
// 这类供应商语义值仍写入 uncertainItems，避免类型漂移触发无意义的模型纠错重试。
func normalizeVisionGrade(payload map[string]any) {
	raw, ok := payload["grade"]
	if !ok {
		return
	}
	if number, ok := numericValue(raw); ok && !math.IsNaN(number) && !math.IsInf(number, 0) && number >= 0 && math.Trunc(number) == number {
		payload["grade"] = int(number)
		return
	}
	if text, ok := raw.(string); ok {
		if number, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && number >= 0 {
			payload["grade"] = number
			return
		}
	}
	delete(payload, "grade")
	uncertain := flexibleStrings(payload["uncertainItems"])
	uncertain = appendUniqueStrings(uncertain, "grade: 供应商返回了无法唯一映射为数字年级的值")
	payload["uncertainItems"] = uncertain
}

func appendUniqueStrings(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

// normalizeVisionExpressions 将供应商的 name/value 表达式对象收敛为领域协议要求的 raw 字符串。
// 这里仅拼接响应中明确给出的字段，不根据题意猜测缺失的数学内容。
func normalizeVisionExpressions(payload map[string]any) {
	raw, ok := payload["expressions"]
	if !ok {
		return
	}
	items, ok := raw.([]any)
	if !ok {
		if object, isObject := raw.(map[string]any); isObject {
			items = []any{object}
		} else {
			return
		}
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := object["raw"].(string); ok && strings.TrimSpace(text) != "" {
			continue
		}
		name, _ := object["name"].(string)
		value, _ := object["value"].(string)
		unit, _ := object["unit"].(string)
		name, value, unit = strings.TrimSpace(name), strings.TrimSpace(value), strings.TrimSpace(unit)
		if value == "" {
			// latex、expression、text 是供应商对公式原文的常见命名；只在字段明确存在时落入 raw。
			for _, key := range []string{"latex", "expression", "text"} {
				if formula, ok := object[key].(string); ok && strings.TrimSpace(formula) != "" {
					object["raw"] = strings.TrimSpace(formula)
					break
				}
			}
			continue
		}
		if name == "" {
			continue
		}
		if unit != "" {
			value += unit
		}
		object["raw"] = name + "=" + value
	}
	payload["expressions"] = items
}

// normalizeVisionRelations 只归一化供应商关系对象的字段类型，不把关系语义扩散到领域协议之外。
// target 在真实响应中既可能是 string 也可能是 string[]；数组化后才能由 decodeRelationsText 安全提取。
func normalizeVisionRelations(payload map[string]any) {
	items, ok := payload["relations"].([]any)
	if !ok {
		return
	}
	for _, item := range items {
		relation, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if target, ok := relation["target"]; ok {
			relation["target"] = flexibleStrings(target)
		}
	}
}

func normalizeVisionFigure(figure map[string]any) []string {
	var uncertain []string
	for _, key := range []string{"id", "figureNo"} {
		if value, ok := normalizedStringValue(figure[key]); ok {
			figure[key] = value
		}
	}
	if labels := normalizeVisionLabels(figure["points"]); len(labels) > 0 {
		figure["points"] = labels
	}
	if labels := normalizeVisionLabels(figure["segments"]); len(labels) > 0 {
		figure["segments"] = labels
	}
	if intersections := normalizeVisionIntersections(figure["intersections"]); intersections != nil {
		figure["intersections"] = intersections
	}
	if collinear := normalizeVisionCollinear(figure["collinear"]); collinear != nil {
		figure["collinear"] = collinear
	}
	if rawShape, ok := figure["shape"]; ok {
		shape := canonicalDiagramShape(rawShape)
		if shape == "" {
			delete(figure, "shape")
		} else {
			figure["shape"] = shape
		}
	}
	normalizeVisionCurves(figure)
	if rawHints, ok := figure["layoutHints"]; ok {
		if hints := normalizeLayoutHints(rawHints); len(hints) > 0 {
			figure["layoutHints"] = hints
		} else {
			delete(figure, "layoutHints")
		}
	}
	constraints, _ := figure["constraints"].([]any)
	keptConstraints := make([]any, 0, len(constraints))
	for _, item := range constraints {
		if text, ok := item.(string); ok {
			uncertain = append(uncertain, "figure "+firstFlexibleString(figure["id"])+" 的约束未能结构化："+strings.TrimSpace(text))
			continue
		}
		if constraint, ok := item.(map[string]any); ok {
			normalizeVisionConstraint(constraint)
			keptConstraints = append(keptConstraints, constraint)
		}
	}
	if constraints != nil {
		figure["constraints"] = keptConstraints
	}
	return uncertain
}

func normalizeVisionLabels(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return flexibleStrings(value)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, strings.TrimSpace(text))
			continue
		}
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := normalizedStringValue(object["name"]); ok {
			result = append(result, name)
			continue
		}
		from, fromOK := normalizedStringValue(object["from"])
		to, toOK := normalizedStringValue(object["to"])
		if fromOK && toOK {
			result = append(result, from+to)
		}
	}
	return result
}

func normalizeVisionIntersections(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			parts := strings.SplitN(text, "=", 2)
			if len(parts) != 2 {
				continue
			}
			point := strings.TrimSpace(parts[0])
			labels := flexibleStrings(strings.FieldsFunc(parts[1], func(r rune) bool { return r == '∩' || r == '、' || r == ',' }))
			result = append(result, map[string]any{"point": point, "of": labels})
			continue
		}
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		point := firstFlexibleString(object["point"])
		if point == "" {
			point = firstFlexibleString(object["at"])
		}
		of := flexibleStrings(object["of"])
		if len(of) == 0 {
			of = flexibleStrings(object["segments"])
		}
		if len(of) == 0 {
			of = flexibleStrings(object["lines"])
		}
		result = append(result, map[string]any{"point": point, "of": of})
	}
	return result
}

func normalizeVisionCollinear(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, splitVisionPoints(text))
			continue
		}
		if object, ok := item.(map[string]any); ok {
			result = append(result, map[string]any{"points": flexibleStrings(object["points"])})
		}
	}
	return result
}

func splitVisionPoints(text string) []string {
	return flexibleStrings(strings.FieldsFunc(text, func(r rune) bool { return r == '、' || r == ',' || r == '-' || r == '，' }))
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// normalizeVisionCurves 统一曲线原语，并兼容历史响应仅返回 shape=semicircle 的情况。
// 只有同时存在明确直径、圆心和半圆形状时才补出 arc，避免根据图片比例臆造曲线。
func normalizeVisionCurves(figure map[string]any) {
	if raw, ok := figure["curves"]; ok {
		items, ok := raw.([]any)
		if ok {
			for _, item := range items {
				if curve, ok := item.(map[string]any); ok {
					if value, ok := curve["type"].(string); ok {
						curve["type"] = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
					}
					curve["endpoints"] = flexibleStrings(curve["endpoints"])
					if center, ok := normalizedStringValue(curve["center"]); ok {
						curve["center"] = center
					}
				}
			}
		}
		return
	}
	shape, _ := figure["shape"].(string)
	if shape != "semicircle" {
		return
	}
	segments := flexibleStrings(figure["segments"])
	constraints, _ := figure["constraints"].([]any)
	var diameter, center string
	for _, value := range segments {
		if len([]rune(strings.TrimSpace(value))) == 2 {
			diameter = strings.TrimSpace(value)
			break
		}
	}
	for _, raw := range constraints {
		constraint, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typeName, _ := constraint["type"].(string)
		if canonicalConstraintType(typeName) != "midpoint" {
			continue
		}
		points := flexibleStrings(constraint["points"])
		if len(points) == 0 {
			points = flexibleStrings(constraint["point"])
		}
		if len(points) == 1 {
			center = points[0]
		}
	}
	if diameter == "" || center == "" {
		return
	}
	figure["curves"] = []any{map[string]any{
		"type": "arc", "endpoints": []string{string([]rune(diameter)[0]), string([]rune(diameter)[1])},
		"center": center, "sweep": "upper",
	}}
}

func normalizedStringValue(value any) (string, bool) {
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		return text, text != ""
	}
	if number, ok := numericValue(value); ok && !math.IsNaN(number) && !math.IsInf(number, 0) {
		return strconv.FormatFloat(number, 'f', -1, 64), true
	}
	if items := flexibleStrings(value); len(items) > 0 {
		return strings.Join(items, "、"), true
	}
	return "", false
}

func canonicalDiagramShape(value any) string {
	best, bestPriority := "", -1
	for _, candidate := range flexibleStrings(value) {
		shape := strings.ToLower(strings.TrimSpace(candidate))
		shape = strings.NewReplacer("-", "_", " ", "_").Replace(shape)
		shape = map[string]string{
			"righttriangle": "right_triangle", "直角三角形": "right_triangle",
			"矩形": "rectangle", "长方形": "rectangle", "正方形": "square",
			"三角形": "triangle", "四边形": "quadrilateral", "圆": "circle",
			"半圆": "semicircle",
		}[shape]
		if shape == "" {
			shape = strings.ToLower(strings.TrimSpace(candidate))
			shape = strings.NewReplacer("-", "_", " ", "_").Replace(shape)
		}
		priority := map[string]int{
			"square": 100, "rectangle": 95, "right_triangle": 90, "triangle": 80,
			"parallelogram": 70, "rhombus": 70, "trapezoid": 70, "circle": 60,
			"quadrilateral": 20, "polygon": 10,
		}[shape]
		if best == "" || priority > bestPriority {
			best, bestPriority = shape, priority
		}
	}
	return best
}

func normalizeLayoutHints(value any) map[string]any {
	result := map[string]any{}
	var merge func(any)
	merge = func(item any) {
		switch typed := item.(type) {
		case map[string]any:
			if candidate, ok := typed["aspectRatio"]; ok {
				if ratio, valid := normalizeAspectRatio(candidate); valid {
					result["aspectRatio"] = ratio
				}
			}
			if candidate, ok := typed["orientation"]; ok {
				if orientation := normalizeOrientation(candidate); orientation != "" {
					result["orientation"] = orientation
				}
			}
			if anchors, ok := typed["anchors"].(map[string]any); ok {
				merged, _ := result["anchors"].(map[string]any)
				if merged == nil {
					merged = map[string]any{}
				}
				for point, anchor := range anchors {
					// top、bottom_left 等语义标签不是坐标，不能猜测转换；原始值仍保留在 OCR trace 中供追溯。
					if coordinate, valid := normalizeCoordinateAnchor(anchor); valid {
						merged[point] = coordinate
					}
				}
				if len(merged) > 0 {
					result["anchors"] = merged
				}
			}
		case []any:
			for _, child := range typed {
				merge(child)
			}
		case string:
			if orientation := normalizeOrientation(typed); orientation != "" {
				result["orientation"] = orientation
			}
		}
	}
	merge(value)
	return result
}

func normalizeAspectRatio(value any) (float64, bool) {
	ratio, ok := numericValue(value)
	if !ok || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 {
		return 0, false
	}
	return ratio, true
}

// normalizeOrientation 只接受渲染层真正理解的枚举值；模型对朝向的长描述属于弱提示，无法安全映射时直接丢弃。
func normalizeOrientation(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case lower == "portrait" || strings.Contains(lower, "portrait") || strings.Contains(text, "竖"):
		return "portrait"
	case lower == "landscape" || strings.Contains(lower, "landscape") || strings.Contains(text, "横"):
		return "landscape"
	default:
		return ""
	}
}

func normalizeCoordinateAnchor(value any) (map[string]any, bool) {
	anchor, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	x, xOK := numericValue(anchor["x"])
	y, yOK := numericValue(anchor["y"])
	if !xOK || !yOK {
		return nil, false
	}
	return map[string]any{"x": x, "y": y}, true
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func normalizeVisionConstraint(constraint map[string]any) {
	typeName, _ := constraint["type"].(string)
	typeName = canonicalConstraintType(typeName)
	constraint["type"] = typeName

	for _, field := range []string{"points", "lines", "segments"} {
		if value, ok := constraint[field]; ok {
			constraint[field] = flexibleStrings(value)
		}
	}
	if typeName == "right_angle" {
		constraint["points"] = normalizeRightAngleVertices(flexibleStrings(constraint["points"]))
	}
	for _, field := range []string{"angle", "value"} {
		if normalized, ok := normalizedStringValue(constraint[field]); ok {
			constraint[field] = normalized
		}
	}
	if rays := constraint["rays"]; rays != nil {
		lines := flexibleStrings(rays)
		constraint["lines"] = appendUniqueStringsFieldValue(constraint["lines"], lines)
		delete(constraint, "rays")
	}
	if vertex := firstFlexibleString(constraint["vertex"]); vertex != "" {
		constraint["points"] = appendUniqueStringsFieldValue(constraint["points"], []string{vertex})
		delete(constraint, "vertex")
	}
	point := firstFlexibleString(constraint["point"])
	at := firstFlexibleString(constraint["at"])
	if point != "" {
		constraint["point"] = point
	}
	if at != "" {
		constraint["at"] = at
		appendUniqueStringField(constraint, "points", at)
	}
	if point != "" {
		appendUniqueStringField(constraint, "points", point)
	}

	segment := firstFlexibleString(constraint["segment"])
	if segment != "" {
		constraint["segment"] = segment
		appendUniqueStringField(constraint, "segments", segment)
	}
	on := flexibleStrings(constraint["on"])
	of := flexibleStrings(constraint["of"])
	switch typeName {
	case "right_angle", "perpendicular", "parallel":
		appendUniqueStringsField(constraint, "lines", of)
	default:
		appendUniqueStringsField(constraint, "segments", of)
	}
	appendUniqueStringsField(constraint, "segments", on)
	if firstFlexibleString(constraint["segment"]) == "" {
		segments := flexibleStrings(constraint["segments"])
		if len(segments) > 0 && (typeName == "midpoint" || typeName == "point_on_segment" || typeName == "point_on_line" || typeName == "extension") {
			constraint["segment"] = segments[0]
		}
	}
	delete(constraint, "of")
	delete(constraint, "on")
}

func appendUniqueStringsFieldValue(existing any, values []string) []string {
	return uniqueStrings(append(flexibleStrings(existing), values...))
}

func normalizeRightAngleVertices(points []string) []string {
	result := make([]string, 0, len(points))
	seen := make(map[string]bool, len(points))
	for _, point := range points {
		runes := []rune(strings.TrimSpace(point))
		if len(runes) == 3 {
			// 三字母角 ABC 的顶点是中间字母 B；这是记号归一化，不是对图形位置的推断。
			point = string(runes[1])
		}
		point = strings.TrimSpace(point)
		if point != "" && !seen[point] {
			result = append(result, point)
			seen[point] = true
		}
	}
	return result
}

func canonicalConstraintType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("-", "_", " ", "_").Replace(normalized)
	aliases := map[string]string{
		"rightangle": "right_angle", "直角": "right_angle",
		"pointonsegment": "point_on_segment", "在线段上": "point_on_segment",
		"pointonline": "point_on_line", "在直线上": "point_on_line",
		"垂直": "perpendicular", "平行": "parallel", "中点": "midpoint", "延长线": "extension",
	}
	if canonical := aliases[normalized]; canonical != "" {
		return canonical
	}
	return normalized
}

func flexibleStrings(value any) []string {
	var result []string
	switch typed := value.(type) {
	case string:
		if item := strings.TrimSpace(typed); item != "" {
			result = append(result, item)
		}
	case json.Number:
		if item := strings.TrimSpace(typed.String()); item != "" {
			result = append(result, item)
		}
	case []string:
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
	case []any:
		for _, item := range typed {
			result = append(result, flexibleStrings(item)...)
		}
	}
	return result
}

func firstFlexibleString(value any) string {
	items := flexibleStrings(value)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func appendUniqueStringField(target map[string]any, field, value string) {
	if value != "" {
		appendUniqueStringsField(target, field, []string{value})
	}
}

func appendUniqueStringsField(target map[string]any, field string, values []string) {
	current := flexibleStrings(target[field])
	seen := make(map[string]bool, len(current)+len(values))
	for _, item := range current {
		seen[item] = true
	}
	for _, item := range values {
		if item = strings.TrimSpace(item); item != "" && !seen[item] {
			current = append(current, item)
			seen[item] = true
		}
	}
	if len(current) > 0 {
		target[field] = current
	}
}
