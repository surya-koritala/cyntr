package tools

import (
	"math"
	"strings"
)

// SimpleEmbedding generates a bag-of-words vector for text similarity.
// Not as good as neural embeddings but works without external APIs.
type SimpleEmbedding struct {
	vocabulary map[string]int // word -> index
	idf        map[string]float64
}

func NewSimpleEmbedding() *SimpleEmbedding {
	return &SimpleEmbedding{
		vocabulary: make(map[string]int),
		idf:        make(map[string]float64),
	}
}

// tokenize splits text into lowercase words.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	word := ""
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			word += string(r)
		} else {
			if len(word) > 2 { // skip short words
				tokens = append(tokens, word)
			}
			word = ""
		}
	}
	if len(word) > 2 {
		tokens = append(tokens, word)
	}
	return tokens
}

// BuildVocabulary builds vocabulary from a corpus of documents.
func (e *SimpleEmbedding) BuildVocabulary(documents []string) {
	docCount := float64(len(documents))
	wordDocFreq := make(map[string]int)

	for _, doc := range documents {
		seen := make(map[string]bool)
		for _, word := range tokenize(doc) {
			if !seen[word] {
				wordDocFreq[word]++
				seen[word] = true
			}
		}
	}

	idx := 0
	for word, freq := range wordDocFreq {
		e.vocabulary[word] = idx
		e.idf[word] = math.Log(docCount / float64(freq+1))
		idx++
	}
}

// Embed generates a TF-IDF vector for the given text.
func (e *SimpleEmbedding) Embed(text string) []float64 {
	if len(e.vocabulary) == 0 {
		return nil
	}
	vec := make([]float64, len(e.vocabulary))
	tokens := tokenize(text)
	tokenCount := float64(len(tokens))
	if tokenCount == 0 {
		return vec
	}

	// Count term frequencies
	tf := make(map[string]int)
	for _, t := range tokens {
		tf[t]++
	}

	// Compute TF-IDF
	for word, count := range tf {
		if idx, ok := e.vocabulary[word]; ok {
			vec[idx] = (float64(count) / tokenCount) * e.idf[word]
		}
	}

	return vec
}

// CosineSimilarity computes the cosine similarity between two vectors.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
