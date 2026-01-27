"""Tests for the set-> macro in macros.hy

The set-> macro provides convenient multi-assignment syntax with support for:
- Simple variables: (set-> x 42)
- Nested attributes: (set-> obj.foo.bar 1)
- Computed keys: (set-> (get d "key") "value")
- Augmented assignment: (set-> x += 5)

Note: Bracket syntax like d["key"] is NOT valid Hy syntax.
Use (get d "key") for dictionary/list access.
"""

import types

import hy
import pytest


# Helper to evaluate Hy code with the macro loaded
def eval_hy(code: str):
    """Evaluate Hy code with macros.hy required.

    Uses a module scope to ensure class definitions persist
    across multiple statements in the same code block.
    """
    full_code = f"""
    (require orch_monitor.macros [set->])
    {code}
    """
    # Create a fresh module scope for each test to avoid pollution
    mod = types.ModuleType("test_module")
    return hy.eval(hy.read_many(full_code), mod.__dict__)


class TestSetMacroSimpleAssignment:
    """Test simple variable assignment."""

    def test_simple_variable(self):
        result = eval_hy("""
        (setv x 0)
        (set-> x 42)
        x
        """)
        assert result == 42

    def test_multiple_simple_variables(self):
        result = eval_hy("""
        (setv a 0 b 0 c 0)
        (set-> a 1
               b 2
               c 3)
        [a b c]
        """)
        assert result == [1, 2, 3]


class TestSetMacroNestedAttributes:
    """Test nested attribute assignment."""

    def test_single_level_attribute(self):
        result = eval_hy("""
        (defclass Obj []
          (defn __init__ [self]
            (setv self.value 0)))
        (setv obj (Obj))
        (set-> obj.value 100)
        obj.value
        """)
        assert result == 100

    def test_multi_level_attribute(self):
        result = eval_hy("""
        (defclass Inner []
          (defn __init__ [self]
            (setv self.deep 0)))
        (defclass Outer []
          (defn __init__ [self]
            (setv self.inner (Inner))))
        (setv obj (Outer))
        (set-> obj.inner.deep 999)
        obj.inner.deep
        """)
        assert result == 999

    def test_multiple_nested_assignments(self):
        result = eval_hy("""
        (defclass Player []
          (defn __init__ [self]
            (setv self.health 100)
            (setv self.score 0)))
        (setv player (Player))
        (set-> player.health 50
               player.score 1000)
        [player.health player.score]
        """)
        assert result == [50, 1000]


class TestSetMacroComputedKeys:
    """Test dictionary/list access with (get ...) syntax."""

    def test_dict_string_key(self):
        result = eval_hy("""
        (setv d {})
        (set-> (get d "key") "value")
        (get d "key")
        """)
        assert result == "value"

    def test_list_index(self):
        result = eval_hy("""
        (setv items [1 2 3])
        (set-> (get items 1) 99)
        items
        """)
        assert result == [1, 99, 3]

    def test_dict_variable_key(self):
        result = eval_hy("""
        (setv d {})
        (setv key "dynamic")
        (set-> (get d key) "works")
        (get d "dynamic")
        """)
        assert result == "works"

    def test_nested_attr_with_get(self):
        result = eval_hy("""
        (defclass Obj []
          (defn __init__ [self]
            (setv self.items {})))
        (setv obj (Obj))
        (set-> (get obj.items "weapon") "sword")
        (get obj.items "weapon")
        """)
        assert result == "sword"

    def test_get_then_attr(self):
        result = eval_hy("""
        (defclass Item []
          (defn __init__ [self]
            (setv self.damage 0)))
        (setv inventory {"sword" (Item)})
        (set-> (. (get inventory "sword") damage) 50)
        (. (get inventory "sword") damage)
        """)
        assert result == 50

    def test_multiple_get_assignments(self):
        result = eval_hy("""
        (setv d {"a" 1 "b" 2 "c" 3})
        (set-> (get d "a") 10
               (get d "b") 20
               (get d "c") 30)
        [(get d "a") (get d "b") (get d "c")]
        """)
        assert result == [10, 20, 30]


class TestSetMacroBracketSyntax:
    """Test bracket [obj key] shorthand for (get obj key)."""

    def test_bracket_dict_string_key(self):
        result = eval_hy("""
        (setv d {})
        (set-> [d "key"] "value")
        (get d "key")
        """)
        assert result == "value"

    def test_bracket_list_index(self):
        result = eval_hy("""
        (setv items [1 2 3])
        (set-> [items 1] 99)
        items
        """)
        assert result == [1, 99, 3]

    def test_bracket_variable_key(self):
        result = eval_hy("""
        (setv d {})
        (setv key "dynamic")
        (set-> [d key] "works")
        (get d "dynamic")
        """)
        assert result == "works"

    def test_bracket_nested_attr(self):
        """[obj.items "key"] -> (get obj.items "key")"""
        result = eval_hy("""
        (defclass Obj []
          (defn __init__ [self]
            (setv self.items {})))
        (setv obj (Obj))
        (set-> [obj.items "weapon"] "sword")
        (get obj.items "weapon")
        """)
        assert result == "sword"

    def test_bracket_augmented(self):
        result = eval_hy("""
        (setv stats {"hits" 10 "misses" 5})
        (set-> [stats "hits"] += 1
               [stats "misses"] -= 1)
        [(get stats "hits") (get stats "misses")]
        """)
        assert result == [11, 4]

    def test_bracket_mixed_with_attrs(self):
        result = eval_hy("""
        (defclass Player []
          (defn __init__ [self]
            (setv self.health 100)
            (setv self.inventory {"gold" 50})))
        (setv p (Player))
        (set-> p.health -= 10
               [p.inventory "gold"] += 25)
        [p.health (get p.inventory "gold")]
        """)
        assert result == [90, 75]

    def test_bracket_multiple(self):
        result = eval_hy("""
        (setv d {"a" 1 "b" 2 "c" 3})
        (set-> [d "a"] 10
               [d "b"] 20
               [d "c"] 30)
        [(get d "a") (get d "b") (get d "c")]
        """)
        assert result == [10, 20, 30]


class TestSetMacroAugmentedAssignment:
    """Test augmented assignment operators."""

    def test_plus_equals(self):
        result = eval_hy("""
        (setv x 10)
        (set-> x += 5)
        x
        """)
        assert result == 15

    def test_minus_equals(self):
        result = eval_hy("""
        (setv x 100)
        (set-> x -= 30)
        x
        """)
        assert result == 70

    def test_multiply_equals(self):
        result = eval_hy("""
        (setv x 7)
        (set-> x *= 3)
        x
        """)
        assert result == 21

    def test_divide_equals(self):
        result = eval_hy("""
        (setv x 20)
        (set-> x /= 4)
        x
        """)
        assert result == 5.0

    def test_floor_divide_equals(self):
        result = eval_hy("""
        (setv x 17)
        (set-> x //= 5)
        x
        """)
        assert result == 3

    def test_modulo_equals(self):
        result = eval_hy("""
        (setv x 17)
        (set-> x %= 5)
        x
        """)
        assert result == 2

    def test_power_equals(self):
        result = eval_hy("""
        (setv x 2)
        (set-> x **= 10)
        x
        """)
        assert result == 1024

    def test_bitwise_and_equals(self):
        result = eval_hy("""
        (setv x 0b1111)
        (set-> x &= 0b1010)
        x
        """)
        assert result == 0b1010

    def test_bitwise_or_equals(self):
        result = eval_hy("""
        (setv x 0b1010)
        (set-> x |= 0b0101)
        x
        """)
        assert result == 0b1111

    def test_augmented_on_nested_attr(self):
        result = eval_hy("""
        (defclass Player []
          (defn __init__ [self]
            (setv self.health 100)
            (setv self.score 0)))
        (setv player (Player))
        (set-> player.health -= 25
               player.score += 500)
        [player.health player.score]
        """)
        assert result == [75, 500]

    def test_augmented_on_get(self):
        result = eval_hy("""
        (setv stats {"hits" 10 "misses" 5})
        (set-> (get stats "hits") += 1
               (get stats "misses") -= 1)
        [(get stats "hits") (get stats "misses")]
        """)
        assert result == [11, 4]


class TestSetMacroMixedAssignment:
    """Test mixing simple and augmented assignment."""

    def test_mixed_simple_and_augmented(self):
        result = eval_hy("""
        (defclass Game []
          (defn __init__ [self]
            (setv self.level 1)
            (setv self.score 0)
            (setv self.player-name "")))
        (setv game (Game))
        (set-> game.level += 1
               game.score 1000
               game.player-name "Hero")
        [game.level game.score game.player-name]
        """)
        assert result == [2, 1000, "Hero"]

    def test_mixed_attrs_and_gets(self):
        result = eval_hy("""
        (defclass Player []
          (defn __init__ [self]
            (setv self.name "")
            (setv self.stats {"hp" 100 "mp" 50})))
        (setv p (Player))
        (set-> p.name "Alice"
               (get p.stats "hp") -= 10
               (get p.stats "mp") += 20)
        [p.name (get p.stats "hp") (get p.stats "mp")]
        """)
        assert result == ["Alice", 90, 70]


class TestSetMacroComplexScenarios:
    """Test complex real-world scenarios."""

    def test_game_state_update(self):
        result = eval_hy("""
        (defclass Position []
          (defn __init__ [self]
            (setv self.x 0)
            (setv self.y 0)))
        (defclass Player []
          (defn __init__ [self]
            (setv self.pos (Position))
            (setv self.health 100)
            (setv self.inventory {"potions" 3 "gold" 50})))
        (setv player (Player))
        
        ;; Simulate: move, take damage, use potion, gain gold
        (set-> player.pos.x += 10
               player.pos.y += 5
               player.health -= 20
               (get player.inventory "potions") -= 1
               (get player.inventory "gold") += 25)
        
        [player.pos.x player.pos.y player.health
         (get player.inventory "potions") (get player.inventory "gold")]
        """)
        assert result == [10, 5, 80, 2, 75]

    def test_nested_dict_in_object(self):
        result = eval_hy("""
        (defclass Config []
          (defn __init__ [self]
            (setv self.settings {"ui" {"theme" "light" "font-size" 12}
                                 "game" {"difficulty" "normal"}})))
        (setv config (Config))
        (set-> (get (get config.settings "ui") "theme") "dark"
               (get (get config.settings "ui") "font-size") 14)
        [(get (get config.settings "ui") "theme") 
         (get (get config.settings "ui") "font-size")]
        """)
        assert result == ["dark", 14]

    def test_list_of_objects(self):
        result = eval_hy("""
        (defclass Enemy []
          (defn __init__ [self hp]
            (setv self.hp hp)))
        (setv enemies [(Enemy 100) (Enemy 80) (Enemy 60)])
        
        ;; Damage all enemies
        (set-> (. (get enemies 0) hp) -= 30
               (. (get enemies 1) hp) -= 20
               (. (get enemies 2) hp) -= 10)
        
        [(. (get enemies 0) hp) (. (get enemies 1) hp) (. (get enemies 2) hp)]
        """)
        assert result == [70, 60, 50]


class TestSetMacroErrorCases:
    """Test error handling."""

    def test_missing_value_raises(self):
        with pytest.raises(Exception):  # SyntaxError wrapped by Hy
            eval_hy("""
            (set-> x)
            """)

    def test_missing_value_after_operator_raises(self):
        with pytest.raises(Exception):  # SyntaxError wrapped by Hy
            eval_hy("""
            (setv x 0)
            (set-> x +=)
            """)

    def test_odd_number_of_args_raises(self):
        with pytest.raises(Exception):  # SyntaxError wrapped by Hy
            eval_hy("""
            (setv x 0 y 0)
            (set-> x 1 y)
            """)
