// Copyright 2019 The Smart Chain Authors
// This file is part of the Smart Chain library.
//
// The Smart Chain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The Smart Chain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the Smart Chain library. If not, see <http://www.gnu.org/licenses/>.

package testing

import (
	"math/rand"
	"time"

	"github.com/yanhuangpai/voyager/pkg/cac"
	"github.com/yanhuangpai/voyager/pkg/infinity"
)

// fixtreuChunks are pregenerated content-addressed chunks necessary for explicit
// test scenarios where random generated chunks are not good enough.
var fixtureChunks = map[string]infinity.Chunk{
	"0025": infinity.NewChunk(
		infinity.MustParseHexAddress("0025737be11979e91654dffd2be817ac1e52a2dadb08c97a7cef12f937e707bc"),
		[]byte{72, 0, 0, 0, 0, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 149, 179, 31, 244, 146, 247, 129, 123, 132, 248, 215, 77, 44, 47, 91, 248, 229, 215, 89, 156, 210, 243, 3, 110, 204, 74, 101, 119, 53, 53, 145, 188, 193, 153, 130, 197, 83, 152, 36, 140, 150, 209, 191, 214, 193, 4, 144, 121, 32, 45, 205, 220, 59, 227, 28, 43, 161, 51, 108, 14, 106, 180, 135, 2},
	),
	"0033": infinity.NewChunk(
		infinity.MustParseHexAddress("0033153ac8cfb0c343db1795f578c15ed8ef827f3e68ed3c58329900bf0d7276"),
		[]byte{72, 0, 0, 0, 0, 0, 0, 0, 170, 117, 0, 0, 0, 0, 0, 0, 21, 157, 63, 86, 45, 17, 166, 184, 47, 126, 58, 172, 242, 77, 153, 249, 97, 5, 107, 244, 23, 153, 220, 255, 254, 47, 209, 24, 63, 58, 126, 142, 41, 79, 201, 182, 178, 227, 235, 223, 63, 11, 220, 155, 40, 181, 56, 204, 91, 44, 51, 185, 95, 155, 245, 235, 187, 250, 103, 49, 139, 184, 46, 199},
	),
	"02c2": infinity.NewChunk(
		infinity.MustParseHexAddress("02c2bd0db71efb7d245eafcc1c126189c1f598feb80e8f14e7ecef913c6a2ef5"),
		[]byte{72, 0, 0, 0, 0, 0, 0, 0, 226, 0, 0, 0, 0, 0, 0, 0, 67, 234, 252, 231, 229, 11, 121, 163, 131, 171, 41, 107, 57, 191, 221, 32, 62, 204, 159, 124, 116, 87, 30, 244, 99, 137, 121, 248, 119, 56, 74, 102, 140, 73, 178, 7, 151, 22, 47, 126, 173, 30, 43, 7, 61, 187, 13, 236, 59, 194, 245, 18, 25, 237, 106, 125, 78, 241, 35, 34, 116, 154, 105, 205},
	),
	"7000": infinity.NewChunk(
		infinity.MustParseHexAddress("70002115a015d40a1f5ef68c29d072f06fae58854934c1cb399fcb63cf336127"),
		[]byte{72, 0, 0, 0, 0, 0, 0, 0, 124, 59, 0, 0, 0, 0, 0, 0, 44, 67, 19, 101, 42, 213, 4, 209, 212, 189, 107, 244, 111, 22, 230, 24, 245, 103, 227, 165, 88, 74, 50, 11, 143, 197, 220, 118, 175, 24, 169, 193, 15, 40, 225, 196, 246, 151, 1, 45, 86, 7, 36, 99, 156, 86, 83, 29, 46, 207, 115, 112, 126, 88, 101, 128, 153, 113, 30, 27, 50, 232, 77, 215},
	),
}

func init() {
	// needed for GenerateTestRandomChunk
	rand.Seed(time.Now().UnixNano())
}

// GenerateTestRandomChunk generates a valid content addressed chunk.
func GenerateTestRandomChunk() infinity.Chunk {
	data := make([]byte, infinity.ChunkSize)
	_, _ = rand.Read(data)
	ch, _ := cac.New(data)
	return ch
}

// GenerateTestRandomInvalidChunk generates a random, however invalid, content
// addressed chunk.
func GenerateTestRandomInvalidChunk() infinity.Chunk {
	data := make([]byte, infinity.ChunkSize)
	_, _ = rand.Read(data)
	key := make([]byte, infinity.SectionSize)
	_, _ = rand.Read(key)
	return infinity.NewChunk(infinity.NewAddress(key), data)
}

// GenerateTestRandomChunks generates a slice of random
// Chunks by using GenerateTestRandomChunk function.
func GenerateTestRandomChunks(count int) []infinity.Chunk {
	chunks := make([]infinity.Chunk, count)
	for i := 0; i < count; i++ {
		chunks[i] = GenerateTestRandomInvalidChunk()
	}
	return chunks
}

// FixtureChunk gets a pregenerated content-addressed chunk and
// panics if one is not found.
func FixtureChunk(prefix string) infinity.Chunk {
	c, ok := fixtureChunks[prefix]
	if !ok {
		panic("no fixture found")
	}
	return c
}
