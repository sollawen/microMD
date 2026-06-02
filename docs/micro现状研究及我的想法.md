# micro的现状

## buffer 与 bufferDisplay 的关系
```
buffer(原始文件里的一行一行的文字) 
-> 现在startLine=15，要显示 
-> bufferDisplay(startLine=15) {
	从buffer里面的第15行开始 {
		把原始的rowTextLine取出来
		计算一下，颜色，语法什么的
		填到screen里面去，从screen的最上面开始往下填
		直到screen填满，就break		
	}
}
```
## 语法高亮

```
buffer -> 语法分析 -> Line.match

显示阶段（每帧 displayBuffer）：
  for 每个可见行 {
      rowTextLine  = buffer.LineBytes(line)       // 原始字节
      styles       = buffer.Line.match            // 每个字符的语法组（缓存在 Line 里）
      for 每个字符 i {
          rune  = rowTextLine[i]
          color = config.GetColor(styles[i])      // 语法组 → 颜色
          screen.SetContent(x, y, rune, color)
      }
  }
```

# 我们的 displayBufferMD ()

## buffer不变
- buffer.LineBytes里面是原始的一行一行的文本。
- buffer.Line.match里语法分析后的颜色数据

## displayBufferMD()的职责
- 把buffer.startLine开始的数据，根据screen的长宽，翻译成要放在screen里面的数据 dispBuffer
	- dispBuffer与buffer之间的行号，要有对应关系的记录
		- 可能 buffer里的一行，对应着dispBuffer里的一行，但也有可对是多行
		- 为了美化而渲染出来的dispBuffer里面的某行，就根本没有对应的buffer里的行号
	- 我们的renders，其实做是负责做这个计算的。
		- 分成 block renders and inline renders
		- block renders 里面会根据自己的需要，自动调用 inline renders
	- 因为我们是block editor, 所以 
		- block renders翻译出来的dispBuffer，不一定只是screen里面的内容，
		- 这个block的完整数据都在dispBuffer里面，有可能有部份数据是在screen之外的
- 翻译完了之后，再把 dispBuffer里面的数据扔到screen里面去

## 用户交互

scroll
- dispBuffer里面是可以找到对应的buffer的行号的，所以上下滚动的时候就方便处理了
	- 如果上边或是下边是个block，因为dispBuffer里面有完整的block的翻译后的数据，就很容易处理了

进入编辑模式（当用户click某个字符的时候，或按任意字母数字等按键的时候）
- 根据click的screen坐标或隐藏光标的坐标，可以从dispBuffer里找到对应的buffer.lineNumber
- 重新渲染，
	- 如果不是block，则这个buffer.lineNumber不做渲染，直接显示最原始的line里的内容
	- 如果是block，则这个block涉及的buffer.lineNumber都不做渲染

