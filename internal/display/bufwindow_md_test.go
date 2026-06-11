package display

import (
	"testing"
)

func TestScreenOffsetToBufferLine(t *testing.T) {
	tests := []struct {
		name         string
		viewportRowBufLine []int
		screenOffset int
		wantLine     int
		wantOK       bool
	}{
		{
			name:         "空数组",
			viewportRowBufLine: nil,
			screenOffset: 0,
			wantLine:     0,
			wantOK:       false,
		},
		{
			name:         "空数组-非零偏移",
			viewportRowBufLine: []int{},
			screenOffset: 5,
			wantLine:     0,
			wantOK:       false,
		},
		{
			name:         "内容行直接命中",
			viewportRowBufLine: []int{0, 1, 2},
			screenOffset: 1,
			wantLine:     1,
			wantOK:       true,
		},
		{
			name:         "内容行-边界",
			viewportRowBufLine: []int{0, 1, 2},
			screenOffset: 0,
			wantLine:     0,
			wantOK:       true,
		},
		{
			name:         "内容行-最后一行",
			viewportRowBufLine: []int{0, 1, 2},
			screenOffset: 2,
			wantLine:     2,
			wantOK:       true,
		},
		{
			name:         "装饰行返回false（语义变更）",
			viewportRowBufLine: []int{-1, 0, -1, 1},
			screenOffset: 0, // -1 装饰行 → (0, false)
			wantLine:     0,
			wantOK:       false,
		},
		{
			name:         "装饰行在中间返回false",
			viewportRowBufLine: []int{-1, 0, -1, 1, -1, 2},
			screenOffset: 2, // -1 装饰行 → (0, false)
			wantLine:     0,
			wantOK:       false,
		},
		{
			name:         "空白填充区域返回false",
			viewportRowBufLine: []int{0, 1, -2, -2},
			screenOffset: 2, // -2 空白 → (0, false)
			wantLine:     0,
			wantOK:       false,
		},
		{
			name:         "越界-负数",
			viewportRowBufLine: []int{0, 1, 2},
			screenOffset: -1,
			wantLine:     0,
			wantOK:       false,
		},
		{
			name:         "越界-超出长度",
			viewportRowBufLine: []int{0, 1, 2},
			screenOffset: 10,
			wantLine:     0,
			wantOK:       false,
		},
		{
			name:         "混合：内容行+装饰行+空白",
			viewportRowBufLine: []int{5, -1, 6, -2, -2},
			screenOffset: 0, // 内容行
			wantLine:     5,
			wantOK:       true,
		},
		{
			name:         "混合：装饰行返回false",
			viewportRowBufLine: []int{5, -1, 6, -2, -2},
			screenOffset: 1, // 装饰行 -1 → (0, false)
			wantLine:     0,
			wantOK:       false,
		},
		{
			name:         "混合：内容行",
			viewportRowBufLine: []int{5, -1, 6, -2, -2},
			screenOffset: 2, // 内容行
			wantLine:     6,
			wantOK:       true,
		},
		{
			name:         "表格：顶边框→header→分隔线→body→底边框",
			viewportRowBufLine: []int{-1, 10, -1, 12, -1},
			screenOffset: 3, // body行=12，直接命中
			wantLine:     12,
			wantOK:       true,
		},
		{
			name:         "表格：click底边框返回false",
			viewportRowBufLine: []int{-1, 10, -1, 12, -1},
			screenOffset: 4, // 装饰行 -1 → (0, false)
			wantLine:     0,
			wantOK:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &BufWindow{viewportRowBufLine: tt.viewportRowBufLine}
			gotLine, gotOK := w.screenOffsetToBufferLine(tt.screenOffset)
			if gotOK != tt.wantOK {
				t.Errorf("screenOffsetToBufferLine() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotLine != tt.wantLine {
				t.Errorf("screenOffsetToBufferLine() line = %d, want %d", gotLine, tt.wantLine)
			}
		})
	}
}

func TestBufferLineToScreenOffset(t *testing.T) {
	tests := []struct {
		name         string
		viewportRowBufLine []int
		bufferLine   int
		wantScreen   int
		wantOK       bool
	}{
		{
			name:         "空数组",
			viewportRowBufLine: nil,
			bufferLine:   0,
			wantScreen:   0,
			wantOK:       false,
		},
		{
			name:         "空数组-非零查找",
			viewportRowBufLine: []int{},
			bufferLine:   5,
			wantScreen:   0,
			wantOK:       false,
		},
		{
			name:         "直接命中-第一个",
			viewportRowBufLine: []int{10, 11, 12},
			bufferLine:   10,
			wantScreen:   0,
			wantOK:       true,
		},
		{
			name:         "直接命中-中间",
			viewportRowBufLine: []int{10, 11, 12},
			bufferLine:   11,
			wantScreen:   1,
			wantOK:       true,
		},
		{
			name:         "直接命中-最后一个（返回最大row）",
			viewportRowBufLine: []int{10, 11, 12},
			bufferLine:   12,
			wantScreen:   2,
			wantOK:       true,
		},
		{
			name:         "多个相同bufferLine返回最后一个",
			viewportRowBufLine: []int{10, 11, 10, 11, 12},
			bufferLine:   10,
			wantScreen:   2, // 最后一个出现的索引
			wantOK:       true,
		},
		{
			name:         "装饰行-1在数组中能找到（返回最后位置）",
			viewportRowBufLine: []int{-1, 0, -1, 1},
			bufferLine:   -1,
			wantScreen:   2, // 最后一个 -1 的位置
			wantOK:       true,
		},
		{
			name:         "空白-2在数组中能找到（返回最后位置）",
			viewportRowBufLine: []int{0, -2, 1, -2},
			bufferLine:   -2,
			wantScreen:   3, // 最后一个 -2 的位置
			wantOK:       true,
		},
		{
			name:         "bufferLine不在viewport内",
			viewportRowBufLine: []int{10, 11, 12},
			bufferLine:   99,
			wantScreen:   0,
			wantOK:       false,
		},
		{
			name:         "bufferLine在viewport外（负数）",
			viewportRowBufLine: []int{10, 11, 12},
			bufferLine:   -1,
			wantScreen:   0,
			wantOK:       false,
		},
		{
			name:         "跨segment连续覆盖",
			viewportRowBufLine: []int{0, 1, 5, 6},
			bufferLine:   5,
			wantScreen:   2,
			wantOK:       true,
		},
		{
			name:         "表格：底边框装饰行不返回",
			viewportRowBufLine: []int{-1, 10, -1, 12, -1},
			bufferLine:   10,
			wantScreen:   1,
			wantOK:       true,
		},
		{
			name:         "表格：body返回最后一个",
			viewportRowBufLine: []int{-1, 10, -1, 12, -1},
			bufferLine:   12,
			wantScreen:   3,
			wantOK:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &BufWindow{viewportRowBufLine: tt.viewportRowBufLine}
			gotScreen, gotOK := w.bufferLineToScreenOffset(tt.bufferLine)
			if gotOK != tt.wantOK {
				t.Errorf("bufferLineToScreenOffset() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotScreen != tt.wantScreen {
				t.Errorf("bufferLineToScreenOffset() screen = %d, want %d", gotScreen, tt.wantScreen)
			}
		})
	}
}