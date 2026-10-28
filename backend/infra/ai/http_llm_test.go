package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func TestVisionPromptDefinesKnownAndAnchorContracts(t *testing.T) {
	if !strings.Contains(visionSystemPrompt, `knowns 必须是 {"name":string,"value":string,"unit":string}[]`) {
		t.Fatal("vision prompt must define the canonical knowns contract")
	}
	if !strings.Contains(visionSystemPrompt, `{"A":{"x":0.2,"y":0.8}}`) || !strings.Contains(visionSystemPrompt, "禁止使用 top、bottom_left") {
		t.Fatal("vision prompt must define coordinate anchors and reject semantic labels")
	}
	for _, field := range []string{"text", "confidence", "fieldConfidence", "analysis", "structuredQuestion", "schemaVersion", "question-vision-v1"} {
		if !strings.Contains(visionSystemPrompt, field) {
			t.Fatalf("vision prompt must preserve the canonical outer response field %q", field)
		}
	}
	if !strings.Contains(visionSystemPrompt, "只执行与 questionKind 对应的一套") || !strings.Contains(visionSystemPrompt, "constraints 必须是对象数组，禁止字符串") {
		t.Fatal("public prompt must isolate type extensions and define one geometry wire protocol")
	}
}

func TestDeepSeekVisionAnalyzerSendsImageAndKeepsFormalQuestion(t *testing.T) {
	adapter := NewDeepSeekVisionAnalyzer(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var payload struct {
			Model    string `json:"model"`
			Thinking struct {
				Type string `json:"type"`
			} `json:"thinking"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode vision request: %v", err)
		}
		var content []struct {
			Type     string `json:"type"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url,omitempty"`
		}
		if err := json.Unmarshal(payload.Messages[1].Content, &content); err != nil {
			t.Fatalf("decode vision content blocks: %v", err)
		}
		if payload.Model != "deepseek-flash" || len(payload.Messages) != 2 || len(content) != 2 {
			t.Fatalf("vision request did not contain text and image blocks: %+v", payload)
		}
		var systemPrompt string
		if err := json.Unmarshal(payload.Messages[0].Content, &systemPrompt); err != nil {
			t.Fatalf("decode system prompt: %v", err)
		}
		if !strings.Contains(strings.ToLower(systemPrompt), "json") {
			t.Fatal("vision system prompt must contain the lowercase json token required by response_format=json_object")
		}
		if payload.Thinking.Type != "disabled" {
			t.Fatalf("vision request must disable thinking, payload=%+v", payload)
		}
		imageBlock := content[1]
		if imageBlock.Type != "image_url" || imageBlock.ImageURL == nil || !strings.HasPrefix(imageBlock.ImageURL.URL, "data:image/png;base64,") {
			t.Fatalf("unexpected image block: %+v", imageBlock)
		}
		return visionHTTPResponse(`{"text":"正式题面：如图……（1）证明……（2）求……","confidence":0.96,"fieldConfidence":{"text":0.96,"diagram":0.94},"structuredQuestion":{"grade":9,"topic":"geometry_numeric","normalizedText":"如图……","tags":["needs_geometry_analysis"],"diagram":{"entities":[{"id":"ABCD","type":"rectangle"}],"relations":[{"type":"rectangle","points":["A","B","C","D"]}],"figures":[{"id":"figure_1","points":["A","B"],"segments":["AB"]}]},"asks":[{"type":"prove","target":["BD⊥EC"]},{"type":"calculate","target":["AE"]}]}}`), nil
	})
	result, err := adapter.Analyze(context.Background(), bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")), "question.png")
	if err != nil || result.Draft.Text == "" || result.StructuredQuestion == nil || result.StructuredQuestion.Topic != "geometry_numeric" {
		t.Fatalf("unexpected vision result: %+v %v", result, err)
	}
	if len(result.StructuredQuestion.Asks) != 2 || result.StructuredQuestion.Asks[0] != "prove：BD⊥EC" || result.StructuredQuestion.Asks[1] != "calculate：AE" {
		t.Fatalf("object asks were not normalized: %+v", result.StructuredQuestion.Asks)
	}
}

func TestDeepSeekVisionAnalyzerRetriesOnceForSchemaDriftAndKeepsTrace(t *testing.T) {
	var calls int
	adapter := NewDeepSeekVisionAnalyzer(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if calls == 2 {
			messages := payload["messages"].([]any)
			last := messages[len(messages)-1].(map[string]any)
			if !strings.Contains(last["content"].([]any)[2].(map[string]any)["text"].(string), "上一次输出未通过结构校验") {
				t.Fatalf("correction instruction was not sent: %+v", last)
			}
			return visionHTTPResponse(`{"text":"题面","confidence":0.95,"structuredQuestion":{"schemaVersion":"question-vision-v1","topic":"arithmetic","normalizedText":"题面","knowns":[],"relations":[],"asks":["求值"],"diagram":{}}}`), nil
		}
		return visionHTTPResponse(`{"text":"题面","confidence":0.95,"structuredQuestion":{"schemaVersion":"question-vision-v1","topic":"arithmetic","normalizedText":"题面","knowns":[],"relations":[],"asks":"求值","diagram":{}}}`), nil
	})
	result, err := adapter.Analyze(context.Background(), bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")), "question.png")
	if err != nil || calls != 2 || result.StructuredQuestion == nil || result.SchemaVersion != VisionSchemaVersion || result.PromptVersion != VisionPromptVersion {
		t.Fatalf("expected one schema correction retry, result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestDeepSeekVisionAnalyzerNormalizesGeometryFieldDriftWithoutRetry(t *testing.T) {
	var calls int
	adapter := NewDeepSeekVisionAnalyzer(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		// 该结构来自真实失败链路：shape、layoutHints 和约束字段均可能偏离 Prompt 中的标量协议。
		return visionHTTPResponse(`{
			"text":"在△ABC中，∠C=90°，AC＞BC，D是AB的中点，过点D作DF⊥DE。",
			"confidence":0.94,
			"structuredQuestion":{
				"schemaVersion":"question-vision-v1",
				"topic":"几何综合题",
				"normalizedText":"在△ABC中，∠C=90°，AC＞BC，D是AB的中点，过点D作DF⊥DE。",
				"knowns":[],
				"relations":[],
				"asks":["求EF"],
				"diagram":{"figures":[{
					"id":"图1",
					"shape":["triangle","right triangle","quadrilateral"],
					"points":["A","B","C","D","E","F"],
					"segments":["AB","BC","CA","DE","DF"],
					"constraints":[
						{"type":"right angle","at":"C","of":["AC","BC"]},
						{"type":"midpoint","point":"D","of":"AB"},
						{"type":"point on segment","point":"F","on":"BC"}
					],
					"layoutHints":[{"aspectRatio":0.72},{"orientation":"portrait"}]
				}]}
			}
		}`), nil
	})

	result, err := adapter.Analyze(context.Background(), bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")), "question.png")
	if err != nil {
		t.Fatalf("geometry field drift should be normalized locally: %v", err)
	}
	if calls != 1 {
		t.Fatalf("recoverable field drift must not consume a model correction retry, calls=%d", calls)
	}
	figure := result.StructuredQuestion.Diagram.Figures[0]
	if figure.Shape != "right_triangle" || figure.LayoutHints == nil || figure.LayoutHints.AspectRatio != 0.72 || figure.LayoutHints.Orientation != "portrait" {
		t.Fatalf("figure semantics were not normalized: %+v", figure)
	}
	if len(figure.Constraints) < 3 || figure.Constraints[0].Type != "right_angle" || len(figure.Constraints[0].Lines) != 2 || figure.Constraints[1].Segment != "AB" || figure.Constraints[2].Segment != "BC" {
		t.Fatalf("constraint aliases were not normalized: %+v", figure.Constraints)
	}
}

func TestDeepSeekVisionAnalyzerNormalizesNonNumericGradeWithoutRetry(t *testing.T) {
	var calls int
	adapter := NewDeepSeekVisionAnalyzer(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return visionHTTPResponse(`{
			"text":"已知：如图，O是半圆的圆心。求证：CD=GF。",
			"confidence":0.96,
			"structuredQuestion":{
				"schemaVersion":"question-vision-v1",
				"grade":"初中",
				"topic":"arithmetic",
				"normalizedText":"已知：如图，O是半圆的圆心。求证：CD=GF。",
				"knowns":[],"relations":[],"asks":["求证：CD=GF"],"diagram":{}
			}
		}`), nil
	})
	result, err := adapter.Analyze(context.Background(), bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")), "question.png")
	if err != nil || result.StructuredQuestion == nil || calls != 1 {
		t.Fatalf("non-numeric grade should be normalized without retry, result=%+v calls=%d err=%v", result, calls, err)
	}
	if result.StructuredQuestion.Grade != 0 {
		t.Fatalf("unmappable grade must not be guessed, got %d", result.StructuredQuestion.Grade)
	}
	found := false
	for _, item := range result.StructuredQuestion.UncertainItems {
		if strings.Contains(item, "grade") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unmappable grade must be recorded in uncertainItems: %+v", result.StructuredQuestion.UncertainItems)
	}
}

func TestDeepSeekVisionAnalyzerDropsFiguresForNonGeometryQuestion(t *testing.T) {
	adapter := NewDeepSeekVisionAnalyzer(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return visionHTTPResponse(`{"text":"函数题","confidence":0.96,"structuredQuestion":{"schemaVersion":"question-vision-v1","topic":"function","normalizedText":"函数题","knowns":[],"relations":[],"asks":["求值"],"diagram":{"figures":[{"id":"figure_1","points":["A","B"],"segments":["AB"]}]}}}`), nil
	})
	result, err := adapter.Analyze(context.Background(), bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")), "question.png")
	if err != nil {
		t.Fatalf("non-geometry response should remain valid: %v", err)
	}
	if result.StructuredQuestion == nil || result.StructuredQuestion.Diagram == nil || len(result.StructuredQuestion.Diagram.Figures) != 0 {
		t.Fatalf("non-geometry question must not expose diagram figures: %+v", result.StructuredQuestion)
	}
}

func TestDeepSeekVisionAnalyzerNormalizesRichAskRelationAndExpressionObjects(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"schemaVersion":"question-vision-v1","topic":"geometry_numeric","normalizedText":"求解","knowns":[],
		"relations":[{"type":"relation","target":"AB"},{"name":"共线","value":"A、B、C"}],
		"asks":[{"name":"求AP1","value":"","unit":"m"},{"type":"proof","target":"AB=CD","task":"证明AB=CD"}],
		"expressions":[{"latex":"\\\\frac{a}{b}","meaning":"比例式"}],
		"diagram":{"figures":[{"points":["A","B"],"segments":["AB"]}]}
	}`), "求解")
	if err != nil {
		t.Fatalf("rich supplier objects should be normalized locally: %v", err)
	}
	if len(question.Asks) != 2 || question.Asks[0] != "求AP1" || question.Asks[1] != "证明AB=CD" {
		t.Fatalf("unexpected asks: %+v", question.Asks)
	}
	if len(question.Relations) != 2 || question.Relations[0] != "relation：AB" || question.Relations[1] != "共线：A、B、C" {
		t.Fatalf("unexpected relations: %+v", question.Relations)
	}
	if len(question.Expressions) != 1 || question.Expressions[0].Raw == "" {
		t.Fatalf("latex expression was not normalized to raw: %+v", question.Expressions)
	}
}

func TestDeepSeekVisionAnalyzerNormalizesNumericConstraintAngle(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric","normalizedText":"求角","knowns":[],"relations":[],"asks":["求角"],
		"diagram":{"figures":[{"points":["A","B","C"],"segments":["AB","BC"],"constraints":[{"type":"angle_equal","points":["A","B","C"],"angle":90}]}]}
	}`), "求角")
	if err != nil {
		t.Fatalf("numeric angle should be normalized locally: %v", err)
	}
	if question.Diagram.Figures[0].Constraints[0].Angle != "90" {
		t.Fatalf("numeric angle was not converted to string: %+v", question.Diagram.Figures[0].Constraints[0])
	}
}

func TestRepairSingleMissingObjectBraceBeforeArrayClose(t *testing.T) {
	content := `{"items":[{"value":1}]}`
	content = strings.Replace(content, `}]}`, `]}`, 1)
	repaired, ok := repairSingleMissingObjectBraceBeforeArrayClose(content)
	if !ok {
		t.Fatal("expected one missing object brace to be repaired")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(repaired), &decoded); err != nil {
		t.Fatalf("repaired JSON is invalid: %v", err)
	}
}

func TestDeepSeekVisionAnalyzerRepairsMissingFigureAndRootBraces(t *testing.T) {
	content := `{"text":"题面","structuredQuestion":{"schemaVersion":"question-vision-v1","topic":"geometry_numeric","normalizedText":"题面","asks":["求值"],"diagram":{"figures":[{"points":["A"],"segments":["AB"],"layoutHints":{"anchors":{"A":{"x":0.1,"y":0.2}}}]} ,"payload":{"kind":"x","data":{"value":1}}}}}`
	content = strings.Replace(content, `}}]} ,`, `}}] ,`, 1)
	content = strings.TrimSuffix(content, "}")
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"finish_reason": "stop", "message": map[string]string{"content": content},
	}}})
	decoded, err := DecodeVisionProviderResponse(raw)
	if err != nil || decoded.StructuredQuestion == nil {
		t.Fatalf("two deterministic brace omissions should be repaired: result=%+v err=%v", decoded, err)
	}
}

func TestDeepSeekVisionAnalyzerNormalizesTextAndExpressionSupplierObjects(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric","normalizedText":"求解","knowns":[],"relations":[],"asks":["求解"],
		"expressions":[{"text":"∠BDE","meaning":"角度"},{"name":"AB","expression":"600"}],
		"diagram":{"figures":[{"points":["A","B"],"segments":["AB"]}]}
	}`), "求解")
	if err != nil {
		t.Fatalf("text/expression objects should be normalized: %v", err)
	}
	if len(question.Expressions) != 2 || question.Expressions[0].Raw != "∠BDE" || question.Expressions[1].Raw != "600" {
		t.Fatalf("unexpected normalized expressions: %+v", question.Expressions)
	}
}

func TestDeepSeekVisionAnalyzerNormalizesPublicPromptGeometryShapes(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry","normalizedText":"证明","knowns":[],"relations":[],
		"asks":[{"type":"prove","statement":"CD=GF"}],
		"diagram":{"figures":[{"points":["A","B","C","D"],
			"segments":[{"from":"A","to":"B"},{"from":"B","to":"C"}],
			"intersections":["D = AB ∩ CD"],"collinear":["A,D,B"],
			"constraints":["A、B、C三点共线",{"type":"right_angle","vertex":"A","rays":[["AB","AC"]],"angle":[90]}]
		}]}
	}`), "证明")
	if err != nil {
		t.Fatalf("public prompt geometry variants should be normalized: %v", err)
	}
	figure := question.Diagram.Figures[0]
	if len(question.Asks) != 1 || question.Asks[0] != "prove：CD=GF" || len(figure.Segments) != 2 || figure.Segments[0] != "AB" || len(figure.Intersections) != 1 || figure.Intersections[0].Point != "D" || len(figure.Collinear) != 1 {
		t.Fatalf("geometry wire fields were not normalized: %+v %+v", question.Asks, figure)
	}
	if len(figure.Constraints) != 1 || figure.Constraints[0].Type != "right_angle" || figure.Constraints[0].Angle != "90" {
		t.Fatalf("string constraint should be quarantined and typed constraint normalized: %+v", figure.Constraints)
	}
	if len(question.UncertainItems) == 0 {
		t.Fatal("unstructured constraint should remain observable as uncertainty")
	}
}

func TestDeepSeekVisionAnalyzerNormalizesLayoutHintTypeDriftWithoutRetry(t *testing.T) {
	var calls int
	adapter := NewDeepSeekVisionAnalyzer(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		// 来自 soak 报告：aspectRatio 被返回为字符串，orientation 被扩展成不可枚举的方向描述。
		return visionHTTPResponse(`{
			"text":"在直角三角形ABC中求EF。",
			"confidence":0.95,
			"structuredQuestion":{
				"schemaVersion":"question-vision-v1",
				"topic":"geometry_numeric",
				"normalizedText":"在直角三角形ABC中求EF。",
				"knowns":[],"relations":[],"asks":["求EF"],
				"diagram":{"figures":[{
					"id":"图1","points":["A","B","C"],"segments":["AB","BC","AC"],
					"layoutHints":{"aspectRatio":"0.75","orientation":"right_triangle_with_AC_vertical_and_BC_horizontal"}
				}]}
			}
		}`), nil
	})

	result, err := adapter.Analyze(context.Background(), bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")), "question.png")
	if err != nil {
		t.Fatalf("layout hint type drift should be recovered locally: %v", err)
	}
	if calls != 1 {
		t.Fatalf("recoverable layout hint drift must not consume a correction retry, calls=%d", calls)
	}
	hints := result.StructuredQuestion.Diagram.Figures[0].LayoutHints
	if hints == nil || hints.AspectRatio != 0.75 || hints.Orientation != "" {
		t.Fatalf("layout hints were not normalized safely: %+v", hints)
	}
}

func TestDecodeVisionQuestionNormalizesNumericFigureNo(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric","normalizedText":"求EF","knowns":[],"relations":[],"asks":["求EF"],
		"diagram":{"figures":[{"figureNo":1,"points":["A","B"],"segments":["AB"]}]}
	}`), "求EF")
	if err != nil {
		t.Fatalf("numeric figureNo should be normalized locally: %v", err)
	}
	if question.Diagram.Figures[0].ID != "1" {
		t.Fatalf("numeric figureNo was not converted to stable string ID: %+v", question.Diagram.Figures[0])
	}
}

func TestDecodeVisionQuestionKeepsIncompleteFigureConstraintsUncertain(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric","normalizedText":"补全图2","knowns":[],"relations":[],"asks":["补全图2"],
		"diagram":{"figures":[{"figureNo":"2","points":["A","B","C","D"],"segments":["AB","BC","AC"],
			"constraints":[
				{"type":"right_angle","points":["A","C","B"],"lines":["AC","BC"]},
				{"type":"perpendicular","points":["D","E","F"],"lines":["DE","DF"]},
				{"type":"point_on_segment","points":["F","B","C"],"segments":["BC"]}
			]
		}]}
	}`), "补全图2")
	if err != nil {
		t.Fatalf("incomplete figure constraints should become uncertainty: %v", err)
	}
	figure := question.Diagram.Figures[0]
	if len(figure.Constraints) != 1 || figure.Constraints[0].Type != "right_angle" {
		t.Fatalf("constraints referencing omitted points must not enter figure: %+v", figure.Constraints)
	}
	if len(question.UncertainItems) == 0 {
		t.Fatalf("omitted-point constraints must be preserved as uncertain items")
	}
}

func TestDeepSeekVisionAnalyzerRepairsSingleMissingRootBraceWithoutRetry(t *testing.T) {
	var calls int
	adapter := NewDeepSeekVisionAnalyzer(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		content := `{"text":"题面","confidence":0.95,"structuredQuestion":{"schemaVersion":"question-vision-v1","topic":"arithmetic","normalizedText":"题面","knowns":[],"relations":[],"asks":["求值"],"diagram":{}}}`
		return visionHTTPResponse(strings.TrimSuffix(content, "}")), nil
	})
	result, err := adapter.Analyze(context.Background(), bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")), "question.png")
	if err != nil || calls != 1 || result.StructuredQuestion == nil {
		t.Fatalf("single missing root brace should be repaired locally: result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestDecodeVisionQuestionNormalizesNamedExpressions(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric","normalizedText":"求EF","knowns":[],"relations":[],"asks":["求EF"],
		"expressions":[{"name":"AE","value":"a","unit":""}],
		"diagram":{"figures":[{"id":"1","points":["A","B"],"segments":["AB"]}]}
	}`), "求EF")
	if err != nil {
		t.Fatalf("named expression should be normalized locally: %v", err)
	}
	if len(question.Expressions) != 1 || question.Expressions[0].Raw != "AE=a" {
		t.Fatalf("named expression was not converted to raw expression: %+v", question.Expressions)
	}
}

func TestDecodeVisionQuestionNormalizesThreeLetterRightAngleVertices(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric","normalizedText":"矩形ABCD","knowns":[],"relations":[],"asks":["求证"],
		"diagram":{"figures":[{"id":"1","points":["A","B","C","D"],"segments":["AB","BC","CD","DA"],
			"constraints":[{"type":"right_angle","points":["DAB","ABC","BCD","CDA"]}]}
		]}
	}`), "矩形ABCD")
	if err != nil {
		t.Fatalf("three-letter right-angle notation should be normalized: %v", err)
	}
	points := question.Diagram.Figures[0].Constraints[0].Points
	if strings.Join(points, ",") != "A,B,C,D" {
		t.Fatalf("right-angle vertices were not normalized: %+v", points)
	}
}

func TestDeepSeekVisionAnalyzerRetriesTruncatedResponse(t *testing.T) {
	var calls int
	adapter := NewDeepSeekVisionAnalyzer(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second, MaxRetries: 0})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return visionCompletion("length", "{"), nil
		}
		return visionHTTPResponse(`{"text":"题面","confidence":0.95,"structuredQuestion":{"schemaVersion":"question-vision-v1","topic":"arithmetic","normalizedText":"题面","knowns":[],"relations":[],"asks":["求值"],"diagram":{}}}`), nil
	})
	result, err := adapter.Analyze(context.Background(), bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")), "question.png")
	if err != nil || calls != 2 || result.FinishReason != "stop" {
		t.Fatalf("expected truncated response correction, result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestDecodeVisionQuestionAcceptsObjectRelationsAndStringKnowns(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"knowns":["AB=1"],
		"relations":[{"type":"perpendicular","lines":["BD","EC"]}],
		"asks":[{"type":"prove","target":["BD⊥EC"]}],
		"diagram":{"figures":[{"id":"图1","points":["A","F","D"],"segments":["AD","EC"],"intersections":[{"point":"F","of":["AD","EC"]}]}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("decode vision question: %v", err)
	}
	if len(question.Knowns) != 1 || question.Knowns[0].Name != "AB=1" {
		t.Fatalf("string known was not normalized: %+v", question.Knowns)
	}
	if len(question.Relations) != 1 || question.Relations[0] != "perpendicular：BD、EC" {
		t.Fatalf("object relation was not normalized: %+v", question.Relations)
	}
	if question.Diagram == nil || len(question.Diagram.Figures) != 1 || len(question.Diagram.Figures[0].Intersections) != 1 || question.Diagram.Figures[0].Intersections[0].Point != "F" {
		t.Fatalf("object intersections were not normalized: %+v", question.Diagram)
	}
}

func TestDecodeVisionQuestionAcceptsLegacyIntersectionMap(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"diagram":{"figures":[{"id":"图1","intersections":{"G":"BD与EC"}}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("decode legacy intersections: %v", err)
	}
	if question.Diagram == nil || len(question.Diagram.Figures) != 1 || len(question.Diagram.Figures[0].Intersections) != 1 || question.Diagram.Figures[0].Intersections[0].Point != "G" {
		t.Fatalf("legacy intersections were not normalized: %+v", question.Diagram)
	}
}

func TestDecodeVisionQuestionAcceptsDeepSeekQuestionTypeAndFigureObjects(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"questionType":"几何综合题",
		"knownConditions":["四边形ABCD是矩形"],
		"relations":["E、A、B共线"],
		"asks":["（1）求证：BD⊥EC"],
		"diagram":{"figures":[{"figureNo":"图1","points":[{"name":"A","coordinates":{}},{"name":"F","coordinates":{}}],"segments":[{"name":"AD"}],"intersections":[{"lines":["EC","AD"],"point":"F"}],"collinear":[{"points":["A","F","D"],"description":"点F在AD上"}]}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("decode DeepSeek figure objects: %v", err)
	}
	figure := question.Diagram.Figures[0]
	if question.Topic != "geometry_numeric" || len(question.Knowns) != 1 || figure.ID != "图1" || len(figure.Points) != 2 || figure.Points[1] != "F" || len(figure.Segments) != 1 || figure.Segments[0] != "AD" || len(figure.Intersections) != 1 || figure.Intersections[0].Of[0] != "EC" || len(figure.Collinear) != 1 {
		t.Fatalf("DeepSeek figure objects were not normalized: %+v", question)
	}
}

func TestDecodeVisionQuestionAcceptsStringExpressions(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"normalizedText":"求证：BD⊥EC",
		"expressions":["AE=AD","BD⊥EC"],
		"asks":["求证：BD⊥EC"],
		"diagram":{"figures":[{"figureNo":"1","points":["A","B"],"segments":["AB"]}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("decode string expressions: %v", err)
	}
	if len(question.Expressions) != 2 || question.Expressions[0].Raw != "AE=AD" || question.Expressions[1].Raw != "BD⊥EC" {
		t.Fatalf("string expressions were not normalized: %+v", question.Expressions)
	}
}

func TestDecodeVisionQuestionPreservesDiagramShapeConstraintsAndLayoutHints(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"diagram":{"figures":[{
			"id":"图1",
			"shape":"rectangle",
			"points":["A","B","C","D"],
			"segments":["AB","BC","CD","DA"],
			"constraints":[{"type":"length_compare","segments":["AB","AD"],"operator":"<"}],
			"layoutHints":{"aspectRatio":0.65,"orientation":"portrait","anchors":{"A":{"x":0.4,"y":0.8}}}
		}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("decode diagram semantics: %v", err)
	}
	figure := question.Diagram.Figures[0]
	if figure.Shape != "rectangle" || len(figure.Constraints) != 1 || figure.Constraints[0].Operator != "<" || figure.LayoutHints == nil || figure.LayoutHints.AspectRatio != 0.65 || figure.LayoutHints.Anchors["A"].X != 0.4 {
		t.Fatalf("diagram semantics were not preserved: %+v", figure)
	}
}

func TestDecodeVisionQuestionDropsSemanticAnchorsAndKeepsCoordinateAnchors(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"normalizedText":"在△ABC中，∠C=90°",
		"knowns":[],
		"relations":[],
		"asks":["求证"],
		"diagram":{"figures":[{
			"id":"图1",
			"points":["A","B","C"],
			"segments":["AB","BC","CA"],
			"layoutHints":{"aspectRatio":0.75,"orientation":"portrait","anchors":{
				"A":"top",
				"B":{"x":0.2,"y":0.9},
				"C":"right_angle_vertex"
			}}
		}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("semantic anchors should not break structured question decoding: %v", err)
	}
	anchors := question.Diagram.Figures[0].LayoutHints.Anchors
	if len(anchors) != 1 || anchors["B"].X != 0.2 || anchors["B"].Y != 0.9 {
		t.Fatalf("only explicit coordinate anchors may enter the domain model: %+v", anchors)
	}
}

func TestDecodeVisionQuestionAcceptsRichKnownObjects(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"normalizedText":"在△ABC中，∠C=90°",
		"knowns":[
			{"id":"k1","text":"在△ABC中，∠C=90°","type":"angle","points":["A","B","C"],"value":"90°"},
			{"name":"AC","value":">BC","unit":""}
		],
		"relations":[],
		"asks":["求证"],
		"diagram":{"figures":[{"id":"图1","points":["A","B","C"],"segments":["AB","BC","CA"]}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("rich known objects should be normalized locally: %v", err)
	}
	if len(question.Knowns) != 2 || question.Knowns[0].Name != "在△ABC中，∠C=90°" || question.Knowns[1].Name != "AC" {
		t.Fatalf("rich known objects were not normalized: %+v", question.Knowns)
	}
}

func TestValidateVisionQuestionRejectsOutOfRangeAnchor(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"normalizedText":"在△ABC中，∠C=90°",
		"knowns":[],
		"relations":[],
		"asks":["求证"],
		"diagram":{"figures":[{
			"id":"图1",
			"points":["A","B","C"],
			"segments":["AB","BC","CA"],
			"layoutHints":{"anchors":{"A":{"x":1.2,"y":0.5}}}
		}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("decode coordinate anchor: %v", err)
	}
	err = validateVisionQuestion(question)
	if err == nil || validationErrorPath(err) != "diagram.figures[0].layoutHints.anchors.A" {
		t.Fatalf("out-of-range anchor must be rejected with a precise path: %v", err)
	}
}

func TestDecodeVisionQuestionAcceptsCompactGeometryConstraintsAndGeneratesFigureIDs(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"diagram":{"figures":[
			{"shape":"triangle","points":["A","B","C","D"],"segments":["AB","BC","CA"],"constraints":[
				{"type":"right_angle","points":["A","C","B"]},
				{"type":"midpoint","point":"D","segment":"AB"}
			]},
			{"shape":"triangle","points":["A","B","C","D"],"segments":["AB","BC","CA"],"constraints":[{"type":"midpoint","point":"D","segment":"AB"}]}
		]}
	}`), "原题")
	if err != nil {
		t.Fatalf("decode compact geometry constraints: %v", err)
	}
	figures := question.Diagram.Figures
	if len(figures) != 2 || figures[0].ID != "figure_1" || figures[1].ID != "figure_2" || figures[0].Constraints[0].Points[1] != "C" || figures[0].Constraints[1].Point != "D" || figures[0].Constraints[1].Segment != "AB" {
		t.Fatalf("compact geometry constraints were not preserved: %+v", figures)
	}
}

func TestDecodeVisionQuestionPromotesTextRelationsToDiagramConstraints(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"knowns":["∠C=90°","AC＞BC","D是AB的中点","DE⊥DF","∠FDE=90°"],
		"relations":[],
		"asks":["求证"],
		"diagram":{"figures":[{"id":"图1","points":["A","B","C","D","E","F"],"segments":["AB","BC","CA","DE","DF"]}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("promote text relations: %v", err)
	}
	constraints := question.Diagram.Figures[0].Constraints
	if len(constraints) != 5 || constraints[0].Type != "right_angle" || constraints[0].Points[0] != "C" || constraints[1].Operator != ">" || constraints[2].Point != "D" || constraints[3].Type != "perpendicular" || constraints[4].Points[0] != "D" {
		t.Fatalf("text relations were not promoted: %+v", constraints)
	}
}

func TestDecodeVisionQuestionPromotesFullWidthEqualsAndNormalizedText(t *testing.T) {
	// 全角“＝”与只出现在题面文本里的关系也必须被提升为图形约束，否则三角形直角顶点会丢失。
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"normalizedText":"在△ABC中，∠C＝90°",
		"knowns":[],
		"relations":[],
		"asks":["求EF"],
		"diagram":{"figures":[{"id":"图1","points":["A","B","C"],"segments":["AB","BC","CA"]}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("promote full-width equals: %v", err)
	}
	constraints := question.Diagram.Figures[0].Constraints
	if len(constraints) != 1 || constraints[0].Type != "right_angle" || constraints[0].Points[0] != "C" {
		t.Fatalf("full-width right angle not promoted from normalizedText: %+v", constraints)
	}
}

func TestDecodeVisionQuestionOnlyPromotesConstraintsToMatchingFigure(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"normalizedText":"在△ABC中，D是AB中点；图1中DF⊥DE。",
		"knowns":["D是AB的中点","DF⊥DE"],
		"relations":[],
		"asks":["求证"],
		"diagram":{"figures":[
			{"id":"图1","points":["A","B","C","D","E","F"],"segments":["AB","BC","CA","DE","DF"]},
			{"id":"图2","points":["A","B","C","D"],"segments":["AB","BC","CA"]}
		]}
	}`), "原题")
	if err != nil {
		t.Fatalf("decode multi-figure geometry: %v", err)
	}
	figures := question.Diagram.Figures
	if len(figures[0].Constraints) != 2 {
		t.Fatalf("figure 1 should retain both applicable constraints: %+v", figures[0].Constraints)
	}
	if len(figures[1].Constraints) != 1 || figures[1].Constraints[0].Type != "midpoint" {
		t.Fatalf("figure 2 must not receive constraints referencing missing E/F: %+v", figures[1].Constraints)
	}
}

func TestValidateVisionQuestionRejectsConstraintReferencingMissingPoint(t *testing.T) {
	question, err := decodeVisionQuestion(json.RawMessage(`{
		"topic":"geometry_numeric",
		"normalizedText":"D是AB的中点",
		"knowns":[],
		"relations":[],
		"asks":["求证"],
		"diagram":{"figures":[{
			"id":"图1",
			"points":["A","B","C"],
			"segments":["AB","BC","CA"],
			"constraints":[{"type":"midpoint","point":"D","segment":"AB"}]
		}]}
	}`), "原题")
	if err != nil {
		t.Fatalf("decode invalid semantic sample: %v", err)
	}
	err = validateVisionQuestion(question)
	if err == nil || validationErrorPath(err) != "diagram.figures[0].constraints[0]" {
		t.Fatalf("missing constraint point must be rejected with a precise path: %v", err)
	}
}

func TestDecodeVisionAnalysisPreservesUncertaintyAndNotes(t *testing.T) {
	analysis := decodeVisionAnalysis(json.RawMessage(`{"uncertainItems":["E位置不清"],"notes":["图2需要补全"]}`))
	if analysis == nil || len(analysis.UncertainItems) != 1 || analysis.UncertainItems[0] != "E位置不清" || len(analysis.Notes) != 1 || analysis.Notes[0] != "图2需要补全" {
		t.Fatalf("analysis metadata was lost: %+v", analysis)
	}
}

func TestHTTPLLMSolverParsesDeepSeekChatCompletion(t *testing.T) {
	adapter := NewHTTPLLMSolver(HTTPLLMConfig{Provider: "deepseek", Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "deepseek-flash" || len(payload.Messages) != 2 {
			t.Fatalf("unexpected DeepSeek request: %+v", payload)
		}
		return response(http.StatusOK, `{"choices":[{"message":{"content":"{\"method\":\"arithmetic\",\"steps\":[\"计算\"],\"expressions\":[\"1+1=2\"],\"finalAnswer\":\"2\"}"}}]}`), nil
	})
	draft, trace, err := adapter.GenerateDraft(context.Background(), domain.StructuredQuestion{Grade: 5, Topic: "arithmetic"}, nil, nil)
	if err != nil || draft.FinalAnswer != "2" || trace.Provider != "deepseek" {
		t.Fatalf("unexpected DeepSeek draft or trace: %+v %+v %v", draft, trace, err)
	}
}

func TestHTTPLLMSolverParsesDeepSeekStreamingJSON(t *testing.T) {
	adapter := NewHTTPLLMSolver(HTTPLLMConfig{Provider: "deepseek", Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second, StreamIdleTimeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Fatalf("DeepSeek tutorial request must enable streaming: %s", body)
		}
		var payload struct {
			Thinking map[string]string `json:"thinking"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Thinking["type"] != "disabled" {
			t.Fatalf("DeepSeek tutorial request must disable thinking, payload=%s", body)
		}
		chunk1, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": `{"method":"geometry",`}}}})
		chunk2, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": `"steps":["证明"],"expressions":["a=b"],"finalAnswer":"成立"}`}, "finish_reason": "stop"}}})
		stream := "data: " + string(chunk1) + "\n\n" + "data: " + string(chunk2) + "\n\n" + "data: [DONE]\n"
		responseBody := response(http.StatusOK, stream)
		responseBody.Header.Set("Content-Type", "text/event-stream")
		return responseBody, nil
	})
	draft, _, err := adapter.GenerateDraft(context.Background(), domain.StructuredQuestion{Topic: "geometry_numeric"}, nil, nil)
	if err != nil || draft.FinalAnswer != "成立" || draft.Method != "geometry" {
		t.Fatalf("expected streamed draft, got draft=%+v err=%v", draft, err)
	}
}

func TestDecodeDraftContentNormalizesDeepSeekContentDraftEnvelope(t *testing.T) {
	content := `{"type":"json_object","content":{"draft":{"problemSummary":"几何题","uncertainties":["比例代换待核验"],"solutionDraft":[{"part":"(1)","steps":[{"text":"由旋转性质得到对应边相等"}],"conclusion":"∠BDE=90°"}],"finalAnswersDraft":["(1) ∠BDE=90°"]}}}`
	draft, report, err := (DeepSeekDraftNormalizer{}).Normalize(content)
	if err != nil || draft.SchemaVersion != "solution-draft-v1" || report.Shape != "$.content.draft:deepseek-structured-draft-v1" || draft.Method == "" || len(draft.Steps) != 1 || len(draft.Expressions) != 1 || draft.FinalAnswer == "" || len(draft.Uncertainties) != 1 {
		t.Fatalf("expected DeepSeek nested draft to normalize, draft=%+v err=%v", draft, err)
	}
}

func TestDeepSeekDraftNormalizerRejectsUnregisteredShape(t *testing.T) {
	_, report, err := (DeepSeekDraftNormalizer{}).Normalize(`{"content":{"answer":{"text":"看起来像答案"}}}`)
	if err == nil || report.Shape != "" || len(report.Warnings) != 1 || report.Warnings[0] != "unsupported_response_shape" {
		t.Fatalf("unknown vendor shape must fail closed, report=%+v err=%v", report, err)
	}
}

func TestDeepSeekDraftNormalizerCoercesStructuredStepObjects(t *testing.T) {
	content := `{"schemaVersion":"solution-draft-v1","method":"geometry","steps":[{"step":1,"desc":"由旋转性质得到AB=AD","expr":"AB=AD"},{"step":2,"desc":"证明两个角相等"}],"expressions":["∠FDP=∠FPD"],"finalAnswer":"DF=PF","uncertainties":[]}`
	draft, report, err := (DeepSeekDraftNormalizer{}).Normalize(content)
	if err != nil || len(draft.Steps) != 2 || draft.Steps[0] != "由旋转性质得到AB=AD" || len(draft.Expressions) != 1 || report.Warnings[0] != "steps_object_items_normalized" {
		t.Fatalf("expected structured steps to normalize into internal strings, draft=%+v report=%+v err=%v", draft, report, err)
	}
}

func TestDeepSeekDraftNormalizerInfersUnregisteredStepFieldSemantics(t *testing.T) {
	content := `{"schemaVersion":"solution-draft-v1","method":"geometry","steps":[{"step":1,"narrative":"由旋转性质得到AB=AD","calculation":"AB=AD"}],"expressions":[],"finalAnswer":"DF=PF","uncertainties":[]}`
	draft, report, err := (DeepSeekDraftNormalizer{}).Normalize(content)
	if err != nil || len(draft.Steps) != 1 || draft.Steps[0] != "由旋转性质得到AB=AD" || len(draft.Expressions) != 1 || draft.Expressions[0] != "AB=AD" || len(report.UnknownPaths) != 0 {
		t.Fatalf("expected unregistered semantic fields to normalize, draft=%+v report=%+v err=%v", draft, report, err)
	}
}

func TestDeepSeekDraftNormalizerRejectsAmbiguousStepFields(t *testing.T) {
	content := `{"schemaVersion":"solution-draft-v1","method":"geometry","steps":[{"step":1,"first":"一种说法","second":"另一种说法"}],"expressions":[],"finalAnswer":"DF=PF","uncertainties":[]}`
	draft, report, err := (DeepSeekDraftNormalizer{}).Normalize(content)
	if err != nil || validateDraft(draft) == nil || len(draft.Steps) != 0 || len(report.UnknownPaths) != 1 || report.UnknownPaths[0] != "steps[0]" {
		t.Fatalf("ambiguous fields must fail closed, draft=%+v report=%+v err=%v", draft, report, err)
	}
}

func TestHTTPLLMSolverParsesDeepSeekFencedDraftWithLeadingText(t *testing.T) {
	adapter := NewHTTPLLMSolver(HTTPLLMConfig{Provider: "deepseek", Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "下面是解题草稿：\n```json\n{\"method\":\"arithmetic\",\"steps\":[\"计算\"],\"expressions\":[\"1+1=2\"],\"finalAnswer\":\"2\"}\n```"}}}})
		return response(http.StatusOK, string(body)), nil
	})
	draft, _, err := adapter.GenerateDraft(context.Background(), domain.StructuredQuestion{Grade: 5, Topic: "arithmetic"}, nil, nil)
	if err != nil || draft.FinalAnswer != "2" {
		t.Fatalf("expected fenced DeepSeek JSON to be normalized, got draft=%+v err=%v", draft, err)
	}
}

func TestHTTPLLMSolverKeepsRawResponseWhenDraftParsingFails(t *testing.T) {
	adapter := NewHTTPLLMSolver(HTTPLLMConfig{Provider: "deepseek", Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"method\":\"arithmetic\"}"}}]}`), nil
	})
	_, trace, err := adapter.GenerateDraft(context.Background(), domain.StructuredQuestion{Grade: 5, Topic: "arithmetic"}, nil, nil)
	if err == nil || trace.RawResponse == "" || !strings.Contains(err.Error(), "must include") {
		t.Fatalf("expected validation error and raw response trace, trace=%+v err=%v", trace, err)
	}
}

func TestHTTPLLMSolverKeepsTraceWhenRequestTimesOutBeforeBody(t *testing.T) {
	adapter := NewHTTPLLMSolver(HTTPLLMConfig{Provider: "deepseek", Endpoint: "http://llm.test", Model: "deepseek-flash", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})
	_, trace, err := adapter.GenerateDraft(context.Background(), domain.StructuredQuestion{Grade: 5, Topic: "geometry_numeric"}, nil, nil)
	if err == nil || trace.Provider != "deepseek" || trace.Model != "deepseek-flash" || trace.RequestID == "" {
		t.Fatalf("expected timeout error with diagnostic trace, trace=%+v err=%v", trace, err)
	}
}

func TestHTTPLLMSolverParsesDraftAndTrace(t *testing.T) {
	adapter := NewHTTPLLMSolver(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "demo", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Request-ID") == "" || req.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("missing request metadata")
		}
		return response(http.StatusOK, `{"draft":{"method":"arithmetic","assumptions":[],"variables":{},"steps":["计算"],"expressions":["1+1=2"],"finalAnswer":"2","uncertainties":[]}}`), nil
	})
	draft, trace, err := adapter.GenerateDraft(context.Background(), domain.StructuredQuestion{Grade: 5, Topic: "arithmetic"}, nil, nil)
	if err != nil || draft.FinalAnswer != "2" || trace.Model != "demo" || trace.PromptVersion != SolutionPromptVersion || trace.RawResponse == "" {
		t.Fatalf("unexpected draft or trace: %+v %+v %v", draft, trace, err)
	}
}

func TestHTTPLLMSolverSendsOriginalImageWithStructuredQuestion(t *testing.T) {
	adapter := NewHTTPLLMSolver(HTTPLLMConfig{Endpoint: "http://llm.test", Model: "demo", Timeout: time.Second})
	adapter.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var payload struct {
			Question      domain.StructuredQuestion `json:"question"`
			OriginalImage string                    `json:"originalImage"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Question.Topic != "geometry_numeric" || !strings.HasPrefix(payload.OriginalImage, "data:image/png;base64,") {
			t.Fatalf("solver must receive the original image and structured index: %+v", payload)
		}
		return response(http.StatusOK, `{"draft":{"method":"geometry","steps":["看原图确认关系"],"expressions":["perpendicular(AB,AC)"],"finalAnswer":"成立"}}`), nil
	})
	draft, _, err := adapter.GenerateDraftWithImage(context.Background(), domain.StructuredQuestion{Topic: "geometry_numeric"}, nil, nil, bytes.NewReader([]byte("image")), "question.png")
	if err != nil || draft.FinalAnswer != "成立" {
		t.Fatalf("unexpected image-aware draft: %+v %v", draft, err)
	}
}

func TestHTTPLLMSolverRejectsMalformedDraft(t *testing.T) {
	adapter := NewHTTPLLMSolver(HTTPLLMConfig{Endpoint: "http://llm.test"})
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"draft":{"method":"arithmetic","finalAnswer":"2"}}`), nil
	})
	_, _, err := adapter.GenerateDraft(context.Background(), domain.StructuredQuestion{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "must include") {
		t.Fatalf("expected draft validation error, got %v", err)
	}
}

func visionHTTPResponse(content string) *http.Response { return visionCompletion("stop", content) }

func visionCompletion(finishReason, content string) *http.Response {
	body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"finish_reason": finishReason,
		"message":       map[string]string{"content": content},
	}}})
	return response(http.StatusOK, string(body))
}
