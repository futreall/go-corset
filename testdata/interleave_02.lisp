(defcolumns X Y)
(interleave Z (X Y))
;; Z[k]+1 == Z[k+1] || Z[k] == Z[k+1]
(defconstraint c1 () (* (- (+ 1 Z) (shift Z 1)) (- Z (shift Z 1))))
