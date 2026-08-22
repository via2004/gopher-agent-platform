package rag

import (
	"math"
	"sort"
)

func SearchSimilar(
	chunks []Chunk,
	query []float32,
	topK int,
) ([]Chunk, error) {
	if topK <= 0 {
		return nil, ErrInvalidTopK
	}
	// 连要搜索的向量都没有，无法计算相似度
	if len(query) == 0 {
		return nil, ErrInvalidVectorSize
	}
	// 没有可搜索的数据，结果是空列表，不算错误
	if len(chunks) == 0 {
		return []Chunk{}, nil
	}
	if topK > len(chunks) {
		topK = len(chunks)
	}

	queryLen := len(query)
	similarity := make([]float32, len(chunks))

	for i, chunk := range chunks {
		if len(chunk.Vector) != queryLen {
			return nil, ErrInvalidVectorSize
		}
		similar, err := calcSimilarity(query, chunk.Vector)
		if err != nil {
			return nil, err
		}

		similarity[i] = similar
	}

	// 也可以用一个小根堆来维护index, 这里采用排序
	indices := make([]int, len(chunks))
	for i := range indices {
		indices[i] = i
	}
	sort.SliceStable(indices, func(i, j int) bool {
		return similarity[indices[i]] > similarity[indices[j]]
	})

	result := make([]Chunk, topK)
	for i := 0; i < topK; i++ {
		result[i] = chunks[indices[i]]
	}

	return result, nil
}

func calcSimilarity(query, vector []float32) (float32, error) {
	var dotProduct, normA, normB float64
	for i := 0; i < len(query); i++ {
		a, b := float64(query[i]), float64(vector[i])
		dotProduct += a * b
		normA += a * a
		normB += b * b
	}
	if normA == 0 || normB == 0 {
		return 0, ErrInvalidVectorElement
	}
	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))), nil
}
