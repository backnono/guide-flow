package solving

import (
	"context"
	"testing"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

func TestGeometryVerifierChecksPerpendicularAndParallel(t *testing.T) {
	question := domain.StructuredQuestion{
		Topic: "geometry_numeric",
		Diagram: &domain.DiagramGraph{Coordinates: map[string]domain.DiagramPoint{
			"A": {X: "0", Y: "0"}, "B": {X: "2", Y: "0"},
			"C": {X: "0", Y: "2"}, "D": {X: "2", Y: "2"},
		}},
	}
	verified, err := (GeometryVerifier{}).VerifyDraft(context.Background(), question, domain.SolutionDraft{
		Expressions: []string{"perpendicular(AB,AC)", "parallel(AB,CD)"}, FinalAnswer: "成立",
	})
	if err != nil || !verified.IsVerified() {
		t.Fatalf("expected verified geometry result: %+v, err=%v", verified, err)
	}
}

func TestGeometryVerifierRejectsWrongLength(t *testing.T) {
	question := domain.StructuredQuestion{
		Topic: "geometry_numeric",
		Diagram: &domain.DiagramGraph{Coordinates: map[string]domain.DiagramPoint{
			"A": {X: "0", Y: "0"}, "B": {X: "2", Y: "0"},
		}},
	}
	verified, err := (GeometryVerifier{}).VerifyDraft(context.Background(), question, domain.SolutionDraft{
		Expressions: []string{"length(AB)=3"}, FinalAnswer: "3",
	})
	if err != nil || verified.Validation.Status != domain.ValidationNeedsConfirmation {
		t.Fatalf("expected confirmation for invalid geometry draft: %+v, err=%v", verified, err)
	}
}

func TestGeometryVerifierDoesNotInferCoordinates(t *testing.T) {
	verified, err := (GeometryVerifier{}).VerifyDraft(context.Background(), domain.StructuredQuestion{Topic: "geometry_numeric", Diagram: &domain.DiagramGraph{}}, domain.SolutionDraft{
		Expressions: []string{"perpendicular(AB,AC)"}, FinalAnswer: "成立",
	})
	if err != nil || verified.Validation.Status != domain.ValidationUnsupported {
		t.Fatalf("expected unsupported result: %+v, err=%v", verified, err)
	}
}
