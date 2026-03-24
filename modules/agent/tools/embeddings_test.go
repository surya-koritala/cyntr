package tools

import (
	"math"
	"testing"
)

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello World! This is a test.")
	if len(tokens) == 0 {
		t.Fatal("expected tokens")
	}
	// "a" and "is" should be filtered (<=2 chars)
	for _, tok := range tokens {
		if len(tok) <= 2 {
			t.Fatalf("short token not filtered: %q", tok)
		}
	}
}

func TestTokenizeLowercase(t *testing.T) {
	tokens := tokenize("Hello WORLD")
	for _, tok := range tokens {
		if tok != "hello" && tok != "world" {
			t.Fatalf("expected lowercase, got %q", tok)
		}
	}
}

func TestSimpleEmbeddingBuildAndEmbed(t *testing.T) {
	emb := NewSimpleEmbedding()
	docs := []string{
		"Deploy the application to production using Docker containers",
		"Query the PostgreSQL database for user records",
		"Set up monitoring with CloudWatch metrics and alarms",
	}
	emb.BuildVocabulary(docs)

	if len(emb.vocabulary) == 0 {
		t.Fatal("vocabulary should not be empty")
	}

	vec := emb.Embed("Deploy application to production")
	if vec == nil {
		t.Fatal("embedding should not be nil")
	}
	if len(vec) != len(emb.vocabulary) {
		t.Fatalf("expected vector length %d, got %d", len(emb.vocabulary), len(vec))
	}
}

func TestCosineSimilaritySameVector(t *testing.T) {
	a := []float64{1, 2, 3}
	sim := CosineSimilarity(a, a)
	if math.Abs(sim-1.0) > 0.001 {
		t.Fatalf("expected ~1.0 for identical vectors, got %f", sim)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{0, 1, 0}
	sim := CosineSimilarity(a, b)
	if sim != 0 {
		t.Fatalf("expected 0 for orthogonal vectors, got %f", sim)
	}
}

func TestCosineSimilarityEmpty(t *testing.T) {
	sim := CosineSimilarity([]float64{}, []float64{})
	if sim != 0 {
		t.Fatal("expected 0 for empty vectors")
	}
}

func TestCosineSimilarityDifferentLengths(t *testing.T) {
	sim := CosineSimilarity([]float64{1, 2}, []float64{1, 2, 3})
	if sim != 0 {
		t.Fatal("expected 0 for different length vectors")
	}
}

func TestSemanticSimilarity(t *testing.T) {
	emb := NewSimpleEmbedding()
	docs := []string{
		"Deploy the application to production using Docker and Kubernetes",
		"Query the PostgreSQL database for user authentication records",
		"Set up monitoring dashboards with CloudWatch metrics and alarms",
		"Configure nginx reverse proxy for load balancing",
	}
	emb.BuildVocabulary(docs)

	queryVec := emb.Embed("How do I deploy to production?")
	deployVec := emb.Embed(docs[0])
	dbVec := emb.Embed(docs[1])

	deploySim := CosineSimilarity(queryVec, deployVec)
	dbSim := CosineSimilarity(queryVec, dbVec)

	// Deploy doc should be more similar to deploy query than database doc
	if deploySim <= dbSim {
		t.Fatalf("deploy doc (%f) should be more similar than db doc (%f) to deploy query", deploySim, dbSim)
	}
}
