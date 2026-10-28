package question

import (
	"encoding/json"
	"fmt"
	"time"
)

type Question struct {
	ID             string            `json:"id"`
	CurrentVersion int64             `json:"currentVersion"`
	Versions       []QuestionVersion `json:"versions,omitempty"`
}

type QuestionVersion struct {
	ID             int64              `json:"id"`
	RawText        string             `json:"rawText"`
	NormalizedText string             `json:"normalizedText,omitempty"`
	Features       StructuredQuestion `json:"features"`
	Confidence     float64            `json:"confidence"`
	CreatedAt      time.Time          `json:"createdAt"`
}

type Known struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

// QuestionAnalysis 是视觉模型与后续路由、求解器之间的稳定契约。
// 关系和问题目标使用结构化对象，避免后续模块再次解析自然语言字符串。
type QuestionAnalysis struct {
	Grade           int                `json:"grade"`
	Subject         string             `json:"subject"`
	Type            string             `json:"type"`
	RawText         string             `json:"rawText"`
	NormalizedText  string             `json:"normalizedText"`
	Entities        []DiagramEntity    `json:"entities"`
	Relations       []QuestionRelation `json:"relations"`
	Knowns          []Known            `json:"knowns"`
	Asks            []QuestionAsk      `json:"asks"`
	Confidence      float64            `json:"confidence"`
	FieldConfidence map[string]float64 `json:"fieldConfidence"`
	UncertainItems  []string           `json:"uncertainItems"`
	// Notes 保存视觉模型对识别范围和歧义的补充说明，不能因供应商字段变化而丢失。
	Notes []string `json:"notes,omitempty"`
}

type QuestionRelation struct {
	Type     string   `json:"type"`
	Points   []string `json:"points,omitempty"`
	Segments []string `json:"segments,omitempty"`
	Lines    []string `json:"lines,omitempty"`
	Point    string   `json:"point,omitempty"`
	Target   []string `json:"target,omitempty"`
}

type QuestionAsk struct {
	Type   string   `json:"type"`
	Target []string `json:"target"`
}

type StructuredQuestion struct {
	// SchemaVersion 固定领域协议版本；供应商字段变化必须在适配层收敛，不能污染下游业务。
	SchemaVersion   string            `json:"schemaVersion"`
	Grade           int               `json:"grade"`
	Topic           string            `json:"topic"`
	NormalizedText  string            `json:"normalizedText,omitempty"`
	KnowledgePoints []string          `json:"knowledgePoints,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	Formula         string            `json:"formula,omitempty"`
	ParameterSchema map[string]string `json:"parameterSchema,omitempty"`
	// UncertainItems 记录模型无法从原图确认的内容，禁止下游静默猜测。
	UncertainItems []string `json:"uncertainItems,omitempty"`
	// Expressions 保存公式的原始形式与可选规范形式，供不同题型模块各自解释。
	Expressions []MathExpression `json:"expressions,omitempty"`
	// Payload 承载题型专属数据；公共流程只依赖外壳，不直接解析 Data。
	Payload   *QuestionPayload `json:"payload,omitempty"`
	Diagram   *DiagramGraph    `json:"diagram,omitempty"`
	Knowns    []Known          `json:"knowns"`
	Relations []string         `json:"relations"`
	Asks      []string         `json:"asks"`
}

// DiagramGraph 是题目知识资产中的图形结构，不依赖 OCR 或视觉模型供应商。
type DiagramGraph struct {
	Entities    []DiagramEntity         `json:"entities,omitempty"`
	Relations   []DiagramRelation       `json:"relations,omitempty"`
	Coordinates map[string]DiagramPoint `json:"coordinates,omitempty"`
	Figures     []DiagramFigure         `json:"figures,omitempty"`
}

// DiagramFigure 表示题目中的一张可复现图形，避免把图1和图2合并成一张图。
type DiagramFigure struct {
	// ID 是图号或模型生成的稳定标识。
	ID string `json:"id"`
	// Title 是前端展示用的图名。
	Title string `json:"title,omitempty"`
	// Points 是图中出现的点标签。
	Points []string `json:"points,omitempty"`
	// Segments 是需要绘制的线段标签，例如 AB、EC、AG。
	Segments []string `json:"segments,omitempty"`
	// Curves 是需要绘制的曲线原语；圆弧必须明确端点、圆心和方向，避免渲染层根据文字猜测几何形状。
	Curves []DiagramCurve `json:"curves,omitempty"`
	// Intersections 记录交点及其所在的线；自定义解码同时兼容旧版键值映射。
	Intersections DiagramIntersections `json:"intersections,omitempty"`
	// Collinear 记录共线点组。
	Collinear [][]string `json:"collinear,omitempty"`
	// Relations 保留模型识别出的其他图形关系。
	Relations []DiagramRelation `json:"relations,omitempty"`
	// Coordinates 只使用题目明确给出的坐标。
	Coordinates map[string]DiagramPoint `json:"coordinates,omitempty"`
	// Shape 是可选的几何形状语义，例如 rectangle、triangle；不等同于渲染坐标。
	Shape string `json:"shape,omitempty"`
	// Constraints 保存平行、垂直、长度比较等数学不变量，供题型校验和布局模块使用。
	Constraints []DiagramConstraint `json:"constraints,omitempty"`
	// LayoutHints 只用于接近原图的示意布局，不能作为数学证明或计算依据。
	LayoutHints *DiagramLayoutHints `json:"layoutHints,omitempty"`
}

// DiagramCurve 表示可复现的圆弧或其他曲线。当前仅要求适配层稳定支持 arc，后续曲线类型可继续扩展。
type DiagramCurve struct {
	// Type 是曲线类型，例如 arc。
	Type string `json:"type"`
	// Endpoints 是曲线端点，圆弧通常包含两个端点。
	Endpoints []string `json:"endpoints,omitempty"`
	// Center 是圆弧的圆心；半圆和圆弧必须提供该字段才能稳定绘制。
	Center string `json:"center,omitempty"`
	// Sweep 描述弧线位于基准弦的哪一侧，例如 upper 或 lower。
	Sweep string `json:"sweep,omitempty"`
}

type DiagramConstraint struct {
	// Type 是约束类型，例如 length_compare、parallel 或 perpendicular。
	Type string `json:"type"`
	// Points 是参与约束的点标签。
	Points []string `json:"points,omitempty"`
	// Point 是单个关键点的兼容写法，例如 midpoint 约束中的 D。
	Point string `json:"point,omitempty"`
	// Lines 是参与约束的直线标签。
	Lines []string `json:"lines,omitempty"`
	// Segments 是参与约束的线段标签。
	Segments []string `json:"segments,omitempty"`
	// Segment 是单条关键线段的兼容写法，例如 midpoint 约束中的 AB。
	Segment string `json:"segment,omitempty"`
	// Operator 是比较关系，例如 <、= 或 >。
	Operator string `json:"operator,omitempty"`
	// At 是“关系发生于某点”的兼容写法，例如 right_angle.at=C。
	At string `json:"at,omitempty"`
	// Value 是供应商附带的自然语言或符号值，仅作追溯，不作为布局依据。
	Value string `json:"value,omitempty"`
	// Angle 是供应商附带的角度值，仅作追溯，不作为布局依据。
	Angle string `json:"angle,omitempty"`
}

// UnmarshalJSON 将视觉模型常见的单值约束归一化为数组约束，避免供应商字段形态影响下游布局。
func (constraint *DiagramConstraint) UnmarshalJSON(data []byte) error {
	type wireConstraint DiagramConstraint
	var value wireConstraint
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*constraint = DiagramConstraint(value)
	if len(constraint.Points) == 0 && constraint.Point != "" {
		constraint.Points = []string{constraint.Point}
	}
	if len(constraint.Segments) == 0 && constraint.Segment != "" {
		constraint.Segments = []string{constraint.Segment}
	}
	if len(constraint.Points) == 0 && constraint.At != "" {
		constraint.Points = []string{constraint.At}
	}
	return nil
}

type DiagramLayoutHints struct {
	// AspectRatio 是示意图宽高比，只影响渲染，不参与数学计算。
	AspectRatio float64 `json:"aspectRatio,omitempty"`
	// Orientation 是示意方向，例如 portrait 或 landscape。
	Orientation string `json:"orientation,omitempty"`
	// Anchors 是点到画布归一化坐标的提示，范围由渲染层校验。
	Anchors map[string]DiagramAnchor `json:"anchors,omitempty"`
}

type DiagramAnchor struct {
	// X 是画布横向归一化坐标。
	X float64 `json:"x"`
	// Y 是画布纵向归一化坐标。
	Y float64 `json:"y"`
}

// DiagramIntersections 兼容视觉模型常返回的对象数组，以及旧实现使用的 map 格式。
// 对外统一输出数组，保证一个交点可以保留多条所在直线关系。
type DiagramIntersections []DiagramIntersection

type DiagramIntersection struct {
	// Point 是交点标签。
	Point string `json:"point"`
	// Of 是包含该交点的线段或直线标签。
	Of []string `json:"of,omitempty"`
}

func (intersection *DiagramIntersection) UnmarshalJSON(data []byte) error {
	var value struct {
		Point string   `json:"point"`
		Of    []string `json:"of"`
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	intersection.Point = value.Point
	intersection.Of = value.Of
	if len(intersection.Of) == 0 {
		intersection.Of = value.Lines
	}
	return nil
}

func (intersections *DiagramIntersections) UnmarshalJSON(data []byte) error {
	var list []DiagramIntersection
	if err := json.Unmarshal(data, &list); err == nil {
		*intersections = list
		return nil
	}
	var legacy map[string]string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	list = make([]DiagramIntersection, 0, len(legacy))
	for point, lines := range legacy {
		list = append(list, DiagramIntersection{Point: point, Of: []string{lines}})
	}
	*intersections = list
	return nil
}

func (figure *DiagramFigure) UnmarshalJSON(data []byte) error {
	var value struct {
		ID            string                  `json:"id"`
		FigureNo      string                  `json:"figureNo"`
		Title         string                  `json:"title"`
		Points        json.RawMessage         `json:"points"`
		Segments      json.RawMessage         `json:"segments"`
		Curves        []DiagramCurve          `json:"curves"`
		Intersections DiagramIntersections    `json:"intersections"`
		Collinear     json.RawMessage         `json:"collinear"`
		Relations     []DiagramRelation       `json:"relations"`
		Coordinates   map[string]DiagramPoint `json:"coordinates"`
		Shape         string                  `json:"shape"`
		Constraints   []DiagramConstraint     `json:"constraints"`
		LayoutHints   *DiagramLayoutHints     `json:"layoutHints"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	points, err := decodeDiagramLabels(value.Points)
	if err != nil {
		return fmt.Errorf("decode figure points: %w", err)
	}
	segments, err := decodeDiagramLabels(value.Segments)
	if err != nil {
		return fmt.Errorf("decode figure segments: %w", err)
	}
	collinear, err := decodeDiagramCollinear(value.Collinear)
	if err != nil {
		return fmt.Errorf("decode figure collinear: %w", err)
	}
	figure.ID = value.ID
	if figure.ID == "" {
		figure.ID = value.FigureNo
	}
	figure.Title = value.Title
	figure.Points = points
	figure.Segments = segments
	figure.Curves = value.Curves
	figure.Intersections = value.Intersections
	figure.Collinear = collinear
	figure.Relations = value.Relations
	figure.Coordinates = value.Coordinates
	figure.Shape = value.Shape
	figure.Constraints = value.Constraints
	figure.LayoutHints = value.LayoutHints
	return nil
}

func decodeDiagramLabels(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var stringsValue []string
	if err := json.Unmarshal(raw, &stringsValue); err == nil {
		return stringsValue, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	labels := make([]string, 0, len(items))
	for _, item := range items {
		var value struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, err
		}
		label := value.Name
		if label == "" {
			label = value.ID
		}
		if label != "" {
			labels = append(labels, label)
		}
	}
	return labels, nil
}

func decodeDiagramCollinear(raw json.RawMessage) ([][]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var groups [][]string
	if err := json.Unmarshal(raw, &groups); err == nil {
		return groups, nil
	}
	var objects []struct {
		Points []string `json:"points"`
	}
	if err := json.Unmarshal(raw, &objects); err != nil {
		return nil, err
	}
	groups = make([][]string, 0, len(objects))
	for _, object := range objects {
		groups = append(groups, object.Points)
	}
	return groups, nil
}

// DiagramPoint 只保存题目明确给出的坐标，不根据图片比例反推坐标，避免把视觉误差当成数学条件。
type DiagramPoint struct {
	X string `json:"x"`
	Y string `json:"y"`
}

type DiagramEntity struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type DiagramRelation struct {
	Type   string   `json:"type"`
	Target string   `json:"target,omitempty"`
	Points []string `json:"points,omitempty"`
}
