# 一、micro的现状

## `displayBuffer()` 的本质

一个 **520+ 行**（`bufwindow.go:382-843`）的函数，逐行逐字符地把 buffer 内容写入 tcell screen：

```
for 每个可见的 buffer 行 {
    画 gutter + 行号
    for 该行的每个字符 {
        获取语法高亮颜色 → screen.SetContent(x, y, rune, style)
    }
}
```

**线性的逐行渲染，没有"块"或"段"的概念。**

但这 520 行里塞了不少东西，**绝对不只是"逐字符写入"那么简单**。至少有 5 层闭包逻辑：

1. **matchbrace 检测**（L404-417）：遍历所有光标查找匹配括号，用于反显/下划线
2. **showchars 解析**（L475-495）：解析 `key1=val1,key2=val2` 格式配置，决定 tab/space 怎么显示
3. **`getRuneStyle` 闭包**（L498-548）：~50 行，处理 tab/空格的 showchars 替换、hltrailingws（尾随空格高亮）、hltaberrors（tab 错误高亮）
4. **`draw` 闭包**（L551-623）：~70 行，处理高亮搜索、选区反显、cursorline 背景色、gutter message、colorcolumn、光标显示
5. **`wrap` 闭包**（L626-643）：处理 softwrap 时 gutter/行号的续行
6. **wordwrap 逻辑**（L654-808）：~150 行，glyph 收集 + 单词换行 + 宽字符处理

**这意味着 `displayBufferMD()` 不是简单地"复用一部分代码"，而是需要在完全理解这些闭包的作用后，决定哪些保留、哪些替换、哪些重构为更通用的接口。** 详见 §六 的特性去留清单。



# 二、我们的 microNeo

- 写一个新的 displayBufferMD()。如果是md文件则不走 displayBuffer()，改使用 displayBufferMD()
- 在 displayBufferMD()里面，实现对markdown的漂亮的渲染


## 原始的文件数据 buffer不变
- buffer.LineBytes里面是原始的一行一行的文本。
- buffer.Line.match里语法分析后的颜色数据

## 渲染片的引入

在《架构设计》里面引入了渲染片的概念
- 其实现有的displayBuffer()里面是有很简单的渲染片的概念的
	- buffer里面的一个超长的line，在softwrap的时候变成了几个row
	- 其实这一组row就是一个最简单的渲染片
- 我们只是引入了更多的渲染片模块
	- single line renders
	- block renders

渲染片的检测与渲染分离，具体见《构架设计》文档

## displayBufferMD()的职责

- 非光标所在的buffer.line, 则使用renders来渲染后，送给screen去显示
- 光标所在的buffer.line, 
	- if editMode then 这一buffer line所在的整个渲染片，使用micro原生的显示方式，送给screen去显示
	- if not editMode then 使用render渲染后，送给screen去显示

### 现有displayBuffer()里面的功能逻辑

1. **matchbrace 检测**（L404-417）：遍历所有光标查找匹配括号，用于反显/下划线
	- 我觉得引入我们的渲染片之后，这个matchbrace应该没什么变化
	- editMode的时候，根据光标所在的字符a，去buffer里面找匹配的字符b，
		- 字符a因为就是光标所在行，所以交给原生逻辑去显示
		- 字符b如果没有落在生效的渲染片里面，就也是原生逻辑去变色显示
		- 字符b如果落在了生效的渲染片里面，就要通过buffer里的坐标，找到渲染片里的坐标，然后变色显示了
	- 非editMode的时候，根本就不存在这个检测动作
	
2. **showchars 解析**（L475-495）：解析 `key1=val1,key2=val2` 格式配置，决定 tab/space 怎么显示
	- editMode的时候，光标所在行的处理完全不变
	- 非editMode的时候，这个tab的显示格式其实渲染片都处理好了
	
3. **`getRuneStyle` 闭包**（L498-548）：~50 行，处理 tab/空格的 showchars 替换、hltrailingws（尾随空格高亮）、hltaberrors（tab 错误高亮）
	- 同上
	
4. **`draw` 闭包**（L551-623）：~70 行，处理高亮搜索、选区反显、cursorline 背景色、gutter message、colorcolumn、光标显示
	- select 反显，见后面的展开说明
	- 高亮搜索，高亮的区域其实就是select区域，用原生的逻辑处理
	- cursorline背景色，只有光标所在的buffer line在editMode时，走原生逻辑处理
	- gutter message 对于markdown文件完全没用
	- 行号和git diff标示，我觉得就按原生逻辑走
	- color column 这东西对markdown的渲染来说没用
	- 光标显示，在editMode时是原生逻辑。非editMode的渲染片里面是不显示光标的

5. **`wrap` 闭包**（L626-643）：处理 softwrap 时 gutter/行号的续行
	- 在editMode时，光标所在行走原生逻辑
	- 其它渲染片，其实也应该学习这个 buffer line 对应的显示方式
	
6. **wordwrap 逻辑**（L654-808）：~150 行，glyph 收集 + 单词换行 + 宽字符处理
	- editMode走原生逻辑
	- 其它渲染片里面由renders来处理


### 关于select的反显。

其实这里有两个场景

editMode，用户在光标所在字符位置上按 shift-方向键等办法，选择了一片连续的文字
- 所有select区域就都是editMode的处理方式，用原生的逻辑处理
	- 比如select只在一个buffer line 里面，简单，交给原生逻辑
	- select 跨越 line10/11/12，这时就和多光标的处理一样：
		- buffer line 10/11/12, 都不走render，都交给原生逻辑处理。
		- 不管是single line renders or block renders，都不走

非editMode：用户在渲染片区域里，用鼠标拖动的操作，选择了一片连续的文字
- 这里就应该由我们的代码来处理了

### 关于渲染片里面行号的显示

single line render 没什么可讲的。
- buffer line number 就是这个渲染片的号号
- 这一行超长了，在渲染片里面变成了两行或多行，gutter的行号显示和现在的softwrap是相同的

block render 就有点复杂，我拿表格来举例说明

```
		|-----------|----------------------|
line5:  |  项目     |  说明				   |
		|-----------|----------------------|
line6:	|microNeo   | 一个好玩的编辑器     |
		|-----------|----------------------|
line7:  |这个个名字 | 这次要换了		   |
		|比较长的项 |					   |
		|目			|					   |
		|-----------|----------------------|
```

