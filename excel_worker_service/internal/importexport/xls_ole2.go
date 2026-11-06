// xls_ole2.go：.xls 解析前置的 OLE2 复合文档校验与规范化。
// extrame/xls 对异常输入容错极差（缺陷 #10）：
//   - 目录缺 "Root Entry" 时短流解引用空指针 panic（SheetJS 等生成器把根目录项写成 "R"）；
//   - FAT 链越界时 StreamReader 直接 log.Fatal 杀死进程，recover 无法兜底；
//   - 无 Workbook 流时返回 (nil, nil)，后续访问空 workbook 再次 panic。
//
// 因此在把文件交给解析器之前，按其读取方式完整校验 OLE2 结构：
// 校验失败一律拒绝（调用方转 1001），绝不进入 BIFF 解析层；校验通过则把
// 根目录项名规范化为 "Root Entry"。剩余的 BIFF 记录层异常由 parseXLS 的
// panic recover 兜底转 1001。
package importexport

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

// ole2Magic 是 OLE2 复合文档（CFB）文件头签名 D0 CF 11 E0 A1 B1 1A E1。
var ole2Magic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

const (
	ole2HeaderBytes   = 512
	ole2SectorBytes   = 512
	ole2DirEntryBytes = 128
	ole2FatEntries    = 128 // 每个 FAT 扇区 128 个 uint32
	ole2DifatEntries  = 127 // DIFAT 扇区前 127 项是 FAT 扇区号，末项是链尾

	ole2EndOfChain = uint32(0xFFFFFFFE)
	ole2FreeSector = uint32(0xFFFFFFFF)

	// extrame/ole2 固定按 512 字节扇区 / 64 字节小扇区寻址（ole.Lsector=512），
	// 声明其他扇区大小的文件会被它读成乱码，必须前置拒绝。
	ole2SectorShift = 9
	ole2MiniShift   = 6

	ole2RootEntryType = 5
	ole2RootEntryName = "Root Entry"

	ole2WorkbookStream = "Workbook"
	ole2BookStream     = "Book"
)

// prepareXLSBytes 校验 .xls 的 OLE2 结构并返回可直接交给 extrame/xls 的副本；
// 结构非法时返回错误，由调用方转换为 1001 契约错误。
func prepareXLSBytes(content []byte) ([]byte, error) {
	if len(content) < ole2HeaderBytes {
		return nil, errors.New("xls: file shorter than OLE2 header")
	}
	if !bytes.HasPrefix(content, ole2Magic) {
		return nil, errors.New("xls: not an OLE2 compound document")
	}
	byteOrder := binary.LittleEndian.Uint16(content[28:30])
	sectorShift := binary.LittleEndian.Uint16(content[30:32])
	miniShift := binary.LittleEndian.Uint16(content[32:34])
	if byteOrder != 0xFFFE || sectorShift != ole2SectorShift || miniShift != ole2MiniShift {
		return nil, errors.New("xls: unsupported OLE2 byte order or sector layout")
	}

	image := decodeOLE2Image(content)
	fat, err := image.buildFAT()
	if err != nil {
		return nil, err
	}
	entries, err := image.decodeDirectory(fat)
	if err != nil {
		return nil, err
	}

	// 与库的 OpenReader 一致：Workbook/Book 与根目录项都取目录序最后一个匹配。
	var book, root *ole2DirEntry
	for index := range entries {
		entry := &entries[index]
		if entry.name == ole2WorkbookStream || entry.name == ole2BookStream {
			book = entry
		}
		if entry.isRoot || entry.name == ole2RootEntryName {
			root = entry
		}
	}
	if book == nil {
		return nil, errors.New("xls: OLE2 directory has no Workbook stream")
	}
	if book.size < image.miniCutoff && root == nil {
		// 短流的解引用需要根目录项，缺失即缺陷 #10 的 panic 栈。
		return nil, errors.New("xls: short Workbook stream without Root Entry")
	}

	// 根目录项即小流容器，其 FAT 链被小流读取器 Seek 时跟进。
	if root != nil {
		if _, err := walkOLE2Chain(root.sstart, fat); err != nil {
			return nil, fmt.Errorf("xls: mini stream container chain broken: %w", err)
		}
	}
	if book.size < image.miniCutoff {
		miniFat, err := image.buildMiniFAT()
		if err != nil {
			return nil, err
		}
		if _, err := walkOLE2Chain(book.sstart, miniFat); err != nil {
			return nil, fmt.Errorf("xls: Workbook mini stream chain broken: %w", err)
		}
	} else {
		if _, err := walkOLE2Chain(book.sstart, fat); err != nil {
			return nil, fmt.Errorf("xls: Workbook stream chain broken: %w", err)
		}
	}

	// 结构合法，规范化根目录项名供解析器按名定位（写 "R" 的 SheetJS 文件因此可解析）。
	normalized := append([]byte(nil), content...)
	for _, entry := range entries {
		if entry.isRoot && entry.name != ole2RootEntryName {
			rewriteOLE2RootName(normalized, entry)
		}
	}
	return normalized, nil
}

// ole2Image 持有文件内容并镜像 ole2.Open 解析出的文件头字段。
type ole2Image struct {
	content      []byte
	totalSectors uint32
	miniCutoff   uint32
	fatCount     uint32
	dirStart     uint32
	miniFatStart uint32
	miniFatCount uint32
	difStart     uint32
}

// decodeOLE2Image 解析文件头字段；头内扇区号的合法性在 buildFAT 中校验。
func decodeOLE2Image(content []byte) *ole2Image {
	image := &ole2Image{
		content:      content,
		totalSectors: uint32((len(content) - ole2HeaderBytes) / ole2SectorBytes),
		miniCutoff:   binary.LittleEndian.Uint32(content[56:60]),
		fatCount:     binary.LittleEndian.Uint32(content[44:48]),
		dirStart:     binary.LittleEndian.Uint32(content[48:52]),
		miniFatStart: binary.LittleEndian.Uint32(content[60:64]),
		miniFatCount: binary.LittleEndian.Uint32(content[64:68]),
		difStart:     binary.LittleEndian.Uint32(content[68:72]),
	}
	return image
}

// buildFAT 镜像 readMSAT 的 FAT 装配：MSAT 声明的 fatCount 个扇区各贡献 128 项；
// DIFAT 链上每个扇区的前 127 项指向 FAT 扇区（越界扇区读到全零，库会照常装配）。
// 原库对 DIFAT 链无环/越界防护，这里拒绝越界链并限制装配规模，防止死循环与内存放大。
func (image *ole2Image) buildFAT() ([]uint32, error) {
	// 头内引用的扇区号必须落在文件内；装配规模上限防止垃圾 DIFAT 链撑爆内存。
	if image.fatCount < 1 || image.fatCount > 109 || image.fatCount > image.totalSectors {
		return nil, errors.New("xls: OLE2 FAT sector count out of range")
	}
	fatLimit := 2*int(image.totalSectors) + 4*109*ole2FatEntries
	fat := make([]uint32, 0, int(image.fatCount)*ole2FatEntries)
	for index := uint32(0); index < image.fatCount; index++ {
		// MSAT 前 109 项位于文件头偏移 76 起，第 index 项即 FAT 扇区号。
		msatEntry := binary.LittleEndian.Uint32(image.content[76+index*4 : 80+index*4])
		if msatEntry >= image.totalSectors {
			return nil, errors.New("xls: OLE2 MSAT entry out of range")
		}
		fat = append(fat, image.sectorValues(msatEntry)...)
	}
	for hop := 0; image.difStart != ole2EndOfChain; hop++ {
		if hop > int(image.totalSectors) || image.difStart >= image.totalSectors {
			return nil, errors.New("xls: OLE2 DIFAT chain broken or looping")
		}
		if len(fat) > fatLimit {
			return nil, errors.New("xls: OLE2 FAT assembly exceeds file bounds")
		}
		difat := image.sectorValues(image.difStart)
		for _, sectorID := range difat[:ole2DifatEntries] {
			fat = append(fat, image.sectorValues(sectorID)...)
		}
		image.difStart = difat[ole2DifatEntries]
	}
	return fat, nil
}

// buildMiniFAT 镜像 readMSAT 的小 FAT 装配：库的循环每次迭代都从
// miniFatStart 扇区取前 127 项（其链尾推进被丢弃，属库的既有行为）。
// 小 FAT 扇区数不会超过文件总扇区数，超出即拒绝，防止垃圾头部放大内存。
func (image *ole2Image) buildMiniFAT() ([]uint32, error) {
	miniFat := make([]uint32, 0)
	if image.miniFatCount == 0 || image.miniFatStart == ole2EndOfChain {
		return miniFat, nil
	}
	if image.miniFatStart >= image.totalSectors || image.miniFatCount > image.totalSectors {
		return nil, errors.New("xls: OLE2 mini FAT layout out of range")
	}
	entries := image.sectorValues(image.miniFatStart)[:ole2DifatEntries]
	for index := uint32(0); index < image.miniFatCount; index++ {
		miniFat = append(miniFat, entries...)
	}
	return miniFat, nil
}

// sectorValues 镜像 ole2 的扇区读取：越界/尾部不完整扇区按全零补齐，
// 与库内 binary.Read 的部分解码行为一致。
func (image *ole2Image) sectorValues(sectorID uint32) []uint32 {
	values := make([]uint32, ole2FatEntries)
	offset := ole2HeaderBytes + int(sectorID)*ole2SectorBytes
	if sectorID >= image.totalSectors || offset >= len(image.content) {
		return values
	}
	sector := image.content[offset:min(offset+ole2SectorBytes, len(image.content))]
	for index := 0; index*4+4 <= len(sector); index++ {
		values[index] = binary.LittleEndian.Uint32(sector[index*4 : index*4+4])
	}
	return values
}

// walkOLE2Chain 沿 FAT 链跟进，拒绝 FREESECT 中断、越界跳转与环，返回链上扇区号。
// 原库在链值越界时经 log.Fatal 直接退出进程，该路径由此封死。
func walkOLE2Chain(start uint32, sat []uint32) ([]uint32, error) {
	if start == ole2EndOfChain {
		return nil, nil
	}
	if start == ole2FreeSector {
		return nil, errors.New("xls: stream starts at FREESECT")
	}
	chain := make([]uint32, 0, len(sat))
	visited := make([]bool, len(sat))
	for sid := start; sid != ole2EndOfChain; {
		if sid == ole2FreeSector {
			return nil, errors.New("xls: FAT chain hits FREESECT")
		}
		if sid >= uint32(len(sat)) {
			return nil, errors.New("xls: FAT chain jumps out of FAT bounds")
		}
		if visited[sid] {
			return nil, errors.New("xls: FAT chain loops")
		}
		visited[sid] = true
		chain = append(chain, sid)
		sid = sat[sid]
	}
	return chain, nil
}

// ole2DirEntry 是镜像库 binary.Read 布局的 128 字节目录项（顺序打包，无对齐填充）。
type ole2DirEntry struct {
	name   string
	isRoot bool
	sstart uint32
	size   uint32
	// sectorID 与 offset 记录条目在文件中的位置，规范化重写根目录项名时使用。
	sectorID uint32
	offset   int
}

// decodeDirectory 镜像 ListDir：沿目录流顺序读取目录项，遇到 EMPTY 条目即止；
// 名字长度异常的条目会让库的 Name() 切片下溢，提前拒绝。
func (image *ole2Image) decodeDirectory(fat []uint32) ([]ole2DirEntry, error) {
	sectors, err := walkOLE2Chain(image.dirStart, fat)
	if err != nil {
		return nil, fmt.Errorf("xls: OLE2 directory chain broken: %w", err)
	}
	entries := make([]ole2DirEntry, 0, len(sectors)*4)
	for _, sectorID := range sectors {
		sectorStart := ole2HeaderBytes + int(sectorID)*ole2SectorBytes
		sectorEnd := min(sectorStart+ole2SectorBytes, len(image.content))
		for offset := 0; sectorStart+offset+ole2DirEntryBytes <= sectorEnd; offset += ole2DirEntryBytes {
			entryBytes := image.content[sectorStart+offset : sectorStart+offset+ole2DirEntryBytes]
			if entryBytes[66] == 0 { // EMPTY：与 ListDir 一致，目录到此为止
				return entries, nil
			}
			entry, err := decodeOLE2DirEntry(entryBytes, sectorID, offset)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// decodeOLE2DirEntry 按库的布局解码目录项；名字取 UTF-16LE 且不含终止符。
func decodeOLE2DirEntry(entryBytes []byte, sectorID uint32, offset int) (ole2DirEntry, error) {
	nameBytes := int(binary.LittleEndian.Uint16(entryBytes[64:66]))
	// Name() 对 Bsize<2 会算出负切片下标而 panic，这里提前拒绝。
	if nameBytes < 2 || nameBytes > 64 {
		return ole2DirEntry{}, errors.New("xls: OLE2 directory entry name length invalid")
	}
	units := make([]uint16, nameBytes/2-1)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(entryBytes[index*2 : index*2+2])
	}
	return ole2DirEntry{
		name:     string(utf16.Decode(units)),
		isRoot:   entryBytes[66] == ole2RootEntryType,
		sstart:   binary.LittleEndian.Uint32(entryBytes[116:120]),
		size:     binary.LittleEndian.Uint32(entryBytes[120:124]),
		sectorID: sectorID,
		offset:   offset,
	}, nil
}

// rewriteOLE2RootName 把根目录项名重写为 "Root Entry"（UTF-16LE 含终止符）。
func rewriteOLE2RootName(content []byte, entry ole2DirEntry) {
	offset := ole2HeaderBytes + int(entry.sectorID)*ole2SectorBytes + entry.offset
	units := utf16.Encode([]rune(ole2RootEntryName))
	for index := 0; index < 32; index++ {
		if index < len(units) {
			binary.LittleEndian.PutUint16(content[offset+index*2:], units[index])
		} else {
			binary.LittleEndian.PutUint16(content[offset+index*2:], 0)
		}
	}
	binary.LittleEndian.PutUint16(content[offset+64:], uint16((len(units)+1)*2))
}
