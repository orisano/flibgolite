package fb2

import (
	"encoding/xml"
	"fmt"
	"io"

	"github.com/vinser/flibgolite/pkg/conv/epub2"
	"github.com/vinser/flibgolite/pkg/database"
	"github.com/vinser/flibgolite/pkg/rlog"
	"github.com/vinser/u8xml"
)

type FB2Parser struct {
	BookId int64
	DB     *database.DB
	LOG    *rlog.Log
	RC     io.ReadSeekCloser
	*xml.Decoder

	chapterNum int
	parent     *tagStack
}

func (p *FB2Parser) Restart() error {
	if _, err := p.RC.Seek(0, io.SeekStart); err != nil {
		return err
	}
	p.Decoder = u8xml.NewDecoder(p.RC)
	return nil
}

func (p *FB2Parser) links() (map[string]string, error) {
	p.chapterNum = 0

	var (
		links        = map[string]string{}
		bodyName     string
		bodyNum      int
		itemName     string
		sectionDepth int
		// sectionNum     int

		updatePage = func() {
			sectionDepth = 0
			// sectionNum = 0
			if bodyName == "chapter" {
				p.chapterNum++
				itemName = fmt.Sprintf("%s_%d", bodyName, p.chapterNum)
			} else {
				itemName = bodyName
			}
		}
	)

	for {
		token, err := p.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "section":
				if bodyName == "chapter" && sectionDepth == 0 {
					// sectionNum++
					updatePage()
				}
				sectionDepth++

			case "body":
				bodyNum++
				bodyName = "chapter"
				if bodyNum > 1 {
					bodyName = fmt.Sprintf("notes-%d", bodyNum-1)
				}
				updatePage()
			}

			for _, a := range t.Attr {
				if a.Name.Local == "id" && len(a.Value) > 0 {
					links[`#`+a.Value] = itemName
					break
				}
			}

		case xml.EndElement:
			if t.Name.Local == "section" {
				sectionDepth--
			}

		}
	}
	p.chapterNum = 0
	return links, nil
}

func (p *FB2Parser) MakeEpub(wc io.WriteCloser) error {
	links, err := p.links()
	if err != nil {
		return err
	}
	err = p.Restart()
	if err != nil {
		return err
	}
	// defer p.decoder.close()

	epub, err := epub2.New(wc)
	if err != nil {
		return err
	}

	defer epub.Close()

	p.parent = newTagStack()
	p.chapterNum = 0
	bodyNum := 0
	for {
		token, err := p.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if t, ok := token.(xml.StartElement); ok {
			switch t.Name.Local {
			case "description":
				if err = p.parseDescription(epub); err != nil {
					return err
				}

			case "body":
				p.parent.reset()
				p.parent.push("body")
				bodyNum++
				bodyName := "chapter"
				if bodyNum > 1 {
					bodyName = fmt.Sprintf("notes-%d", bodyNum-1)
				}

				if err = p.parseBody(epub, bodyName, links); err != nil {
					return err
				}

			case "binary":
				content, err := p.getText()
				if err != nil {
					return err
				}

				var contentType, id string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "content-type":
						contentType = a.Value
					case "id":
						id = a.Value
					}
				}
				if err = epub.AddBinary(id, contentType, content); err != nil {
					return err
				}
			}
		}
	}

	if err = epub.AddTOC(); err != nil {
		return err
	}

	if err = epub.AddOPF(); err != nil {
		return err
	}
	return nil
}

func (p *FB2Parser) getText() (string, error) {
	token, err := p.Token()
	if err != nil {
		return "", err
	}

	if t, ok := token.(xml.CharData); ok {
		return string(t), nil
	}

	return "", nil
}
