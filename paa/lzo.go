package paa

import "errors"

var errLZO = errors.New("paa: bad LZO data")

// unLZO unpacks LZO1X data (what PAA mipmaps are packed with) into exactly
// size bytes. It is a port of the Linux kernel's lzo1x_decompress_safe,
// checking every read and write.
func unLZO(in []byte, size int) ([]byte, error) {
	out := make([]byte, 0, size)
	ip := 0
	state := 0
	var t, next int

	lit := func(n int) error { // copy n literal bytes
		if n < 0 || ip+n > len(in) || len(out)+n > size {
			return errLZO
		}
		out = append(out, in[ip:ip+n]...)
		ip += n
		return nil
	}
	match := func(back, n int) error { // copy n bytes from back bytes behind
		from := len(out) - back
		if back <= 0 || from < 0 || len(out)+n > size {
			return errLZO
		}
		for k := 0; k < n; k++ {
			out = append(out, out[from+k])
		}
		return nil
	}
	byteAt := func() (int, error) {
		if ip >= len(in) {
			return 0, errLZO
		}
		b := int(in[ip])
		ip++
		return b, nil
	}
	// run reads a zero-run length extension: 255 per zero byte, then a byte.
	run := func(base int) (int, error) {
		n := 0
		for ip < len(in) && in[ip] == 0 {
			n += 255
			ip++
		}
		b, err := byteAt()
		return n + base + b, err
	}

	if len(in) == 0 {
		return nil, errLZO
	}
	if in[0] > 17 {
		t = int(in[0]) - 17
		ip = 1
		if t < 4 {
			if err := lit(t); err != nil {
				return nil, err
			}
			state = t
		} else {
			if err := lit(t); err != nil {
				return nil, err
			}
			state = 4
		}
	}
	for {
		b, err := byteAt()
		if err != nil {
			return nil, err
		}
		t = b
		var back, n int
		switch {
		case t < 16:
			switch state {
			case 0: // a literal run
				if t == 0 {
					if t, err = run(15); err != nil {
						return nil, err
					}
				}
				if err := lit(t + 3); err != nil {
					return nil, err
				}
				state = 4
				continue
			case 4: // a 3-byte match, far
				next = t & 3
				b2, err := byteAt()
				if err != nil {
					return nil, err
				}
				back = 1 + 0x800 + t>>2 + b2<<2
				n = 3
			default: // a 2-byte match, near
				next = t & 3
				b2, err := byteAt()
				if err != nil {
					return nil, err
				}
				back = 1 + t>>2 + b2<<2
				n = 2
			}
		case t >= 64:
			next = t & 3
			b2, err := byteAt()
			if err != nil {
				return nil, err
			}
			back = 1 + (t>>2)&7 + b2<<3
			n = t>>5 + 1
		case t >= 32:
			n = t&31 + 2
			if n == 2 {
				if n, err = run(31 + 2); err != nil {
					return nil, err
				}
			}
			if ip+2 > len(in) {
				return nil, errLZO
			}
			v := int(in[ip]) | int(in[ip+1])<<8
			ip += 2
			back = 1 + v>>2
			next = v & 3
		default: // 16..31
			n = t&7 + 2
			hi := (t & 8) << 11
			if n == 2 {
				if n, err = run(7 + 2); err != nil {
					return nil, err
				}
			}
			if ip+2 > len(in) {
				return nil, errLZO
			}
			v := int(in[ip]) | int(in[ip+1])<<8
			ip += 2
			back = hi + v>>2
			next = v & 3
			if back == 0 { // the end marker
				if len(out) != size {
					return nil, errLZO
				}
				return out, nil
			}
			back += 0x4000
		}
		if err := match(back, n); err != nil {
			return nil, err
		}
		state = next
		if err := lit(next); err != nil {
			return nil, err
		}
	}
}
