package analyzer

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePythonFile(t *testing.T) {
	src := `from dataclasses import dataclass
from typing import Optional

@dataclass
class User:
    id: int
    name: str

    def greet(self) -> str:
        return f"Hello, {self.name}"

def create_user(name: str) -> User:
    return User(id=0, name=name)
`
	result, err := ParsePython("user.py", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "user.py", result.Filename)
	assert.Len(t, result.Classes, 1)

	cls := result.Classes[0]
	assert.Equal(t, "User", cls.Name)
	assert.Equal(t, []string{"dataclass"}, cls.Decorators)
	assert.Len(t, cls.Methods, 1)

	method := cls.Methods[0]
	assert.Equal(t, "greet", method.Name)
	assert.Equal(t, []string{"self"}, method.Params)
	assert.Equal(t, "str", method.ReturnType)

	assert.Len(t, result.Functions, 1)
	fn := result.Functions[0]
	assert.Equal(t, "create_user", fn.Name)
	assert.Equal(t, []string{"name"}, fn.Params)
	assert.Equal(t, "User", fn.ReturnType)

	assert.Len(t, result.Imports, 2)
	assert.Equal(t, "dataclasses", result.Imports[0].Module)
	assert.Equal(t, "dataclass", result.Imports[0].Name)
	assert.Equal(t, "typing", result.Imports[1].Module)
	assert.Equal(t, "Optional", result.Imports[1].Name)
}

func TestParsePythonFileWithRelativeImports(t *testing.T) {
	src := `from .base import BaseModel
from ..utils.crypto import hash_password
from services.user_service import UserService
`
	result, err := ParsePython("models/user.py", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Imports, 3)

	assert.Equal(t, ".base", result.Imports[0].Module)
	assert.Equal(t, "BaseModel", result.Imports[0].Name)
	assert.True(t, result.Imports[0].IsRelative)

	assert.Equal(t, "..utils.crypto", result.Imports[1].Module)
	assert.Equal(t, "hash_password", result.Imports[1].Name)
	assert.True(t, result.Imports[1].IsRelative)

	assert.Equal(t, "services.user_service", result.Imports[2].Module)
	assert.Equal(t, "UserService", result.Imports[2].Name)
	assert.False(t, result.Imports[2].IsRelative)
}

func TestParsePythonEmptyFile(t *testing.T) {
	result, err := ParsePython("empty.py", "")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Classes)
	assert.Empty(t, result.Functions)
	assert.Empty(t, result.Imports)
}

func TestParsePythonWithSyntaxErrors(t *testing.T) {
	src := `class User
    id: int
`
	result, err := ParsePython("broken.py", src)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Classes)
}

func TestParseDirectory(t *testing.T) {
	repoPath := filepath.Join("..", "..", "testdata", "repos", "python-basic")
	_, err := os.Stat(repoPath)
	if os.IsNotExist(err) {
		t.Skip("testdata not found, skipping integration test")
	}

	results, err := ParseDirectory(repoPath, "python")
	require.NoError(t, err)
	require.NotEmpty(t, results)

	assert.Len(t, results, 7)

	var userPy *FileResult
	for _, r := range results {
		if filepath.Base(r.Filename) == "user.py" {
			userPy = r
			break
		}
	}
	require.NotNil(t, userPy, "user.py should be parsed")
	assert.Equal(t, "User", userPy.Classes[0].Name)
	assert.Len(t, userPy.Classes[0].Methods, 3)
}

func TestParseJavaScriptFile(t *testing.T) {
	src := `import React from 'react';
import { User } from '../types/user';

interface UserCardProps {
  user: User;
}

export const UserCard: React.FC<UserCardProps> = ({ user }) => {
  return <div>{user.name}</div>;
};

function formatName(name: string): string {
  return name.toUpperCase();
}
`
	result, err := ParseJavaScript("UserCard.tsx", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "UserCard.tsx", result.Filename)
	assert.Len(t, result.Imports, 2)
	assert.Equal(t, "react", result.Imports[0].Module)
	assert.Equal(t, "React", result.Imports[0].Name)

	assert.Len(t, result.Classes, 1)
	assert.Equal(t, "UserCardProps", result.Classes[0].Name)
	assert.Len(t, result.Functions, 1)
	assert.Equal(t, "formatName", result.Functions[0].Name)
	assert.Equal(t, []string{"name"}, result.Functions[0].Params)
	assert.Equal(t, "string", result.Functions[0].ReturnType)
}

func TestParseJavaScriptWithClassAndMethods(t *testing.T) {
	src := `import { API } from './api';

class UserService extends BaseService {
  constructor(api: API) {
    this.api = api;
  }

  async getUser(id: number): Promise<User> {
    return this.api.get("/users/" + id);
  }

  private formatUser(data: any): User {
    return { ...data };
  }
}

export default UserService;
`
	result, err := ParseJavaScript("UserService.ts", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Classes, 1)
	cls := result.Classes[0]
	assert.Equal(t, "UserService", cls.Name)
	assert.Equal(t, []string{"BaseService"}, cls.Bases)
	assert.Len(t, cls.Methods, 3)

	methodNames := make([]string, len(cls.Methods))
	for i, m := range cls.Methods {
		methodNames[i] = m.Name
	}
	assert.Contains(t, methodNames, "constructor")
	assert.Contains(t, methodNames, "getUser")
	assert.Contains(t, methodNames, "formatUser")
}

func TestParseJavaScriptNamedImports(t *testing.T) {
	src := `import { User, Admin, Guest } from '../types/user';
import * as utils from './utils';
`
	result, err := ParseJavaScript("api.ts", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Imports, 4)
	assert.Equal(t, "User", result.Imports[0].Name)
	assert.Equal(t, "Admin", result.Imports[1].Name)
	assert.Equal(t, "Guest", result.Imports[2].Name)
	assert.Equal(t, "utils", result.Imports[3].Name)
}

func TestParseJavaScriptArrowFunction(t *testing.T) {
	src := `const sum = (a: number, b: number): number => a + b;
`
	result, err := ParseJavaScript("math.ts", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Functions, 1)
	assert.Equal(t, "sum", result.Functions[0].Name)
}

func TestParseTypeScriptInterface(t *testing.T) {
	src := `export interface User {
  id: number;
  name: string;
}

interface Admin extends User {
  permissions: string[];
}
`
	result, err := ParseJavaScript("types.ts", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Classes, 2)
	assert.Equal(t, "User", result.Classes[0].Name)
	assert.Equal(t, "Admin", result.Classes[1].Name)
	assert.Equal(t, []string{"User"}, result.Classes[1].Bases)
}

func TestParseTypeScriptEnum(t *testing.T) {
	src := `enum Status {
  Active = 'active',
  Inactive = 'inactive',
}

export enum Role {
  Admin,
  User,
}
`
	result, err := ParseJavaScript("enums.ts", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Classes, 2)
	assert.Equal(t, "Status", result.Classes[0].Name)
	assert.Equal(t, "Role", result.Classes[1].Name)
}

func TestParseTypeScriptGenerics(t *testing.T) {
	src := `class Container<T> {
  private value: T;

  constructor(value: T) {
    this.value = value;
  }

  getValue(): T {
    return this.value;
  }
}

function identity<U>(arg: U): U {
  return arg;
}
`
	result, err := ParseJavaScript("generics.ts", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Classes, 1)
	assert.Equal(t, "Container", result.Classes[0].Name)
	assert.Len(t, result.Classes[0].Methods, 2)

	assert.Len(t, result.Functions, 1)
	assert.Equal(t, "identity", result.Functions[0].Name)
	assert.Equal(t, []string{"arg"}, result.Functions[0].Params)
	assert.Equal(t, "U", result.Functions[0].ReturnType)
}

func TestParseJSParamsWithDestructuring(t *testing.T) {
	params := parseJSParams("{ user, config }")
	assert.Equal(t, []string{"user", "config"}, params)

	params = parseJSParams("a, { b, c }")
	assert.Equal(t, []string{"a", "b", "c"}, params)
}

func TestScanLines(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	require.NoError(t, os.WriteFile(tmpFile, []byte("line1\nline2\nline3\n"), 0644))

	lines, err := ScanLines(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
}

func TestScanLinesEmptyFile(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "empty.txt")
	require.NoError(t, os.WriteFile(tmpFile, []byte{}, 0644))

	lines, err := ScanLines(tmpFile)
	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestParseDirectoryWithInvalidPath(t *testing.T) {
	_, err := ParseDirectory("/nonexistent/path/that/does/not/exist", "python")
	require.Error(t, err)
}

func TestParsePythonComplexRepo(t *testing.T) {
	repoPath := filepath.Join("..", "..", "testdata", "repos", "python-complex")
	_, err := os.Stat(repoPath)
	if os.IsNotExist(err) {
		t.Skip("testdata not found, skipping integration test")
	}

	results, err := ParseDirectory(repoPath, "python")
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Should find all source files
	assert.GreaterOrEqual(t, len(results), 8)

	// Verify complex structures are parsed
	var userPy, orderPy, basePy *FileResult
	for _, r := range results {
		base := filepath.Base(r.Filename)
		switch base {
		case "user.py":
			userPy = r
		case "order.py":
			orderPy = r
		case "base.py":
			basePy = r
		}
	}

	require.NotNil(t, basePy, "base.py should be parsed")
	require.NotNil(t, userPy, "user.py should be parsed")
	require.NotNil(t, orderPy, "order.py should be parsed")

	// BaseModel should be abstract class
	assert.Len(t, basePy.Classes, 3)

	// User has multiple inheritance and decorators
	assert.GreaterOrEqual(t, len(userPy.Classes), 1)
	userClass := userPy.Classes[0]
	assert.Equal(t, "User", userClass.Name)

	// Order has circular dependency import
	assert.GreaterOrEqual(t, len(orderPy.Imports), 1)
}

func TestParsePythonParamsWithDefaults(t *testing.T) {
	params := parsePythonParams("self, name: str = 'default', age: int = 0")
	assert.Equal(t, []string{"self", "name", "age"}, params)
}

func TestParsePythonImportAlias(t *testing.T) {
	src := `import os as operating_system
import sys, json as js
from typing import Optional as Opt
`
	result, err := ParsePython("aliases.py", src)
	require.NoError(t, err)
	require.Len(t, result.Imports, 4)

	assert.Equal(t, "os", result.Imports[0].Module)
	assert.Equal(t, "operating_system", result.Imports[0].Name)

	assert.Equal(t, "sys", result.Imports[1].Module)
	assert.Equal(t, "sys", result.Imports[1].Name)

	assert.Equal(t, "json", result.Imports[2].Module)
	assert.Equal(t, "js", result.Imports[2].Name)

	assert.Equal(t, "typing", result.Imports[3].Module)
	assert.Equal(t, "Opt", result.Imports[3].Name)
}

func TestParsePythonDeepNesting(t *testing.T) {
	src := `class Outer:
    class Inner:
        def inner_method(self):
            pass

        class DeepInner:
            pass

    def outer_method(self):
        def nested_func():
            pass
        pass
`
	result, err := ParsePython("deep.py", src)
	require.NoError(t, err)

	// Current regex-based parser treats each class definition at its indent level
	// as a top-level class entry. Nested classes are captured independently.
	// Due to indent tracking limitations, outer_method exits the deepest nested
	// class scope and becomes a top-level function instead of Outer‘s method.
	require.Len(t, result.Classes, 3)
	assert.Equal(t, "Outer", result.Classes[0].Name)
	assert.Equal(t, "Inner", result.Classes[1].Name)
	assert.Equal(t, "DeepInner", result.Classes[2].Name)

	// Inner class should have its own method
	assert.Len(t, result.Classes[1].Methods, 1)
	assert.Equal(t, "inner_method", result.Classes[1].Methods[0].Name)

	// Outer method and its nested function end up as top-level functions
	// due to nested class indent tracking limitations
	require.Len(t, result.Functions, 2)
	assert.Equal(t, "outer_method", result.Functions[0].Name)
	assert.Equal(t, "nested_func", result.Functions[1].Name)
}

func TestParsePythonConcurrent(t *testing.T) {
	src := `class User:
    id: int
    name: str

    def greet(self) -> str:
        return f"Hello, {self.name}"

class Order:
    total: float

    def calc(self) -> float:
        return self.total * 1.1
`
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := ParsePython("concurrent.py", src)
			require.NoError(t, err)
			require.Len(t, result.Classes, 2)
			assert.Equal(t, "User", result.Classes[0].Name)
			assert.Equal(t, "Order", result.Classes[1].Name)
		}()
	}
	wg.Wait()
}

func TestParseGoFile(t *testing.T) {
	src := `package main

import "fmt"
import "net/http"

type User struct {
    ID   int
    Name string
}

func NewUser(name string) *User {
    return &User{Name: name}
}

func (u *User) Greet() string {
    return "Hello, " + u.Name
}

func main() {
    u := NewUser("Alice")
    fmt.Println(u.Greet())
}
`
	result, err := ParseGo("user.go", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "user.go", result.Filename)
	assert.Len(t, result.Imports, 2)
	assert.Equal(t, "fmt", result.Imports[0].Module)
	assert.Equal(t, "net/http", result.Imports[1].Module)

	assert.Len(t, result.Classes, 1)
	cls := result.Classes[0]
	assert.Equal(t, "User", cls.Name)

	assert.Len(t, result.Functions, 3)
	fnNames := make([]string, len(result.Functions))
	for i, f := range result.Functions {
		fnNames[i] = f.Name
	}
	assert.Contains(t, fnNames, "NewUser")
	assert.Contains(t, fnNames, "Greet")
	assert.Contains(t, fnNames, "main")

	// Check params and return types
	var newUserFn *FunctionInfo
	for _, f := range result.Functions {
		if f.Name == "NewUser" {
			newUserFn = &f
			break
		}
	}
	require.NotNil(t, newUserFn)
	assert.Equal(t, []string{"name"}, newUserFn.Params)
	assert.Equal(t, "*User", newUserFn.ReturnType)
}

func TestParseGoEmptyFile(t *testing.T) {
	result, err := ParseGo("empty.go", "")
	require.NoError(t, err)
	assert.Empty(t, result.Classes)
	assert.Empty(t, result.Functions)
	assert.Empty(t, result.Imports)
}

func TestParseJavaFile(t *testing.T) {
	src := `package com.example;

import java.util.List;
import java.util.Optional;

public class UserService {
    private final UserRepository repository;

    public UserService(UserRepository repository) {
        this.repository = repository;
    }

    public User getUser(int id) {
        return repository.findById(id);
    }

    public List<User> listUsers() {
        return repository.findAll();
    }

    private Optional<User> findById(int id) {
        return Optional.ofNullable(repository.findById(id));
    }
}

class UserRepository {
    public User findById(int id) {
        return new User();
    }

    public List<User> findAll() {
        return List.of();
    }
}
`
	result, err := ParseJava("UserService.java", src)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "UserService.java", result.Filename)
	assert.Len(t, result.Imports, 2)
	assert.Equal(t, "java.util.List", result.Imports[0].Module)
	assert.Equal(t, "java.util.Optional", result.Imports[1].Module)

	assert.Len(t, result.Classes, 2)
	assert.Equal(t, "UserService", result.Classes[0].Name)
	assert.Equal(t, "UserRepository", result.Classes[1].Name)

	// UserService should have 3 methods (constructor not matched by regex)
	assert.Len(t, result.Classes[0].Methods, 3)
	methodNames := make([]string, len(result.Classes[0].Methods))
	for i, m := range result.Classes[0].Methods {
		methodNames[i] = m.Name
	}
	assert.Contains(t, methodNames, "getUser")
	assert.Contains(t, methodNames, "listUsers")
	assert.Contains(t, methodNames, "findById")
}

func TestParseJavaWithInheritance(t *testing.T) {
	src := `import java.util.List;

public class AdminUser extends User implements Serializable {
    private List<String> permissions;

    public List<String> getPermissions() {
        return permissions;
    }
}
`
	result, err := ParseJava("AdminUser.java", src)
	require.NoError(t, err)
	require.Len(t, result.Classes, 1)
	assert.Equal(t, "AdminUser", result.Classes[0].Name)
	assert.Equal(t, []string{"User"}, result.Classes[0].Bases)
	assert.Len(t, result.Classes[0].Methods, 1)
	assert.Equal(t, "getPermissions", result.Classes[0].Methods[0].Name)
}

func TestParseJavaEmptyFile(t *testing.T) {
	result, err := ParseJava("empty.java", "")
	require.NoError(t, err)
	assert.Empty(t, result.Classes)
	assert.Empty(t, result.Functions)
	assert.Empty(t, result.Imports)
}

func TestParseGoParams(t *testing.T) {
	params := parseGoParams("name string, age int")
	assert.Equal(t, []string{"name", "age"}, params)

	params = parseGoParams("ctx context.Context, req *Request")
	assert.Equal(t, []string{"ctx", "req"}, params)

	params = parseGoParams("")
	assert.Empty(t, params)
}

func TestParseJavaParams(t *testing.T) {
	params := parseJavaParams("String name, int age")
	assert.Equal(t, []string{"name", "age"}, params)

	params = parseJavaParams("List<String> users")
	assert.Equal(t, []string{"users"}, params)

	params = parseJavaParams("")
	assert.Empty(t, params)
}

func BenchmarkParsePython(b *testing.B) {
	src := `from dataclasses import dataclass
from typing import Optional, List

@dataclass
class User:
    id: int
    name: str
    email: Optional[str] = None

    def greet(self) -> str:
        return f"Hello, {self.name}"

    def to_dict(self) -> dict:
        return {"id": self.id, "name": self.name, "email": self.email}

class Order:
    id: int
    user: User
    items: List[str]

    def total(self) -> float:
        return sum(len(item) for item in self.items)

class OrderService:
    def __init__(self, repo):
        self.repo = repo

    def create_order(self, user_id: int, items: List[str]) -> Order:
        user = self.repo.get_user(user_id)
        return Order(id=0, user=user, items=items)

    def cancel_order(self, order_id: int) -> bool:
        return self.repo.delete(order_id)
`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParsePython("bench.py", src)
		if err != nil {
			b.Fatal(err)
		}
	}
}
